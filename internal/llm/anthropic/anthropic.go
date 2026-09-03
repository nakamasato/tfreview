// Package anthropic は Messages API で plan を評価する。
//
// エージェントではなく単発の呼び出しにしているのは「入力は plan の結果だけ」を
// 守るため。ツールを持たせるとリポジトリや外部を見に行けてしまう。
package anthropic

import (
	"context"
	"errors"
	"fmt"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/nakamasato/tfreview/internal/llm"
)

var ErrPlanTooLarge = errors.New("plan exceeds max_plan_chars")

// ErrResponseTruncated は Messages API の応答が max_tokens で打ち切られたことを示す。
// tool_use の Input が不完全な JSON になり ParseAnswers が失敗するため、その一般的な
// パースエラーより先に検知して原因を判定側に伝える。
var ErrResponseTruncated = errors.New("response truncated by max_tokens")

const defaultMaxTokens = 16000

type Options struct {
	Model        string
	MaxPlanChars int
	MaxTokens    int
	APIKey       string
}

type Provider struct {
	opts   Options
	client sdk.Client
}

func New(opts Options) *Provider {
	if opts.MaxTokens == 0 {
		opts.MaxTokens = defaultMaxTokens
	}
	var ro []option.RequestOption
	if opts.APIKey != "" {
		ro = append(ro, option.WithAPIKey(opts.APIKey))
	}
	return &Provider{opts: opts, client: sdk.NewClient(ro...)}
}

func (p *Provider) Name() string  { return "anthropic" }
func (p *Provider) Model() string { return p.opts.Model }

func (p *Provider) Judge(ctx context.Context, req llm.Request) ([]llm.Answer, llm.Usage, error) {
	planJSON := PlanJSON(req.Plan)
	// 桁違いに大きい入力は判定の質が落ちるうえ、上限に当たると 400 で全チェックが
	// 失敗になる。原因が容量だと分かる形で返すほうがレビュアーに届く。
	if len(planJSON) > p.opts.MaxPlanChars {
		return nil, llm.Usage{}, fmt.Errorf("%w: %d > %d chars", ErrPlanTooLarge, len(planJSON), p.opts.MaxPlanChars)
	}

	tool := sdk.ToolParam{
		Name:        toolName,
		Description: sdk.String("Report the verdict for every check id."),
		InputSchema: sdk.ToolInputSchemaParam{
			Properties: map[string]any{
				"verdicts": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"check_id": map[string]any{"type": "string"},
							"verdict":  map[string]any{"type": "string", "enum": []string{"hit", "miss", "unverifiable"}},
							"reason":   map[string]any{"type": "string"},
						},
						"required": []string{"check_id", "verdict", "reason"},
					},
				},
			},
			Required: []string{"verdicts"},
		},
	}

	maxTokens := p.opts.MaxTokens
	if maxTokens == 0 {
		maxTokens = defaultMaxTokens
	}
	resp, err := p.client.Messages.New(ctx, sdk.MessageNewParams{
		Model:     sdk.Model(p.opts.Model),
		MaxTokens: int64(maxTokens),
		System: []sdk.TextBlockParam{{
			Text:         BuildSystem(req.Language),
			CacheControl: sdk.NewCacheControlEphemeralParam(),
		}},
		Messages:   []sdk.MessageParam{sdk.NewUserMessage(sdk.NewTextBlock(BuildUser(req, planJSON)))},
		Tools:      []sdk.ToolUnionParam{{OfTool: &tool}},
		ToolChoice: sdk.ToolChoiceParamOfTool(toolName),
	})
	if err != nil {
		return nil, llm.Usage{}, fmt.Errorf("messages.new: %w", err)
	}
	usage := llm.Usage{
		Calls:            1,
		InputTokens:      resp.Usage.InputTokens,
		CacheWriteTokens: resp.Usage.CacheCreationInputTokens,
		CacheReadTokens:  resp.Usage.CacheReadInputTokens,
		OutputTokens:     resp.Usage.OutputTokens,
	}
	if err := checkTruncated(resp, maxTokens, len(req.Checks)); err != nil {
		return nil, usage, err
	}
	for _, block := range resp.Content {
		if tu, ok := block.AsAny().(sdk.ToolUseBlock); ok {
			answers, err := ParseAnswers(tu.Input)
			return answers, usage, err
		}
	}
	return nil, usage, errors.New("response has no tool_use block")
}

// checkTruncated は、tool_use の Input を不完全な JSON としてパース失敗させる前に
// 「max_tokens で打ち切られた」ことを検知する。ParseAnswers のエラーだけでは容量
// オーバーが原因だと判別できず、レビュアーに伝わらない。
func checkTruncated(resp *sdk.Message, maxTokens int, numChecks int) error {
	if resp.StopReason == sdk.StopReasonMaxTokens {
		return fmt.Errorf("%w: max_tokens=%d, checks=%d", ErrResponseTruncated, maxTokens, numChecks)
	}
	return nil
}
