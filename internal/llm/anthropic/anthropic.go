// Package anthropic evaluates a plan via the Messages API.
//
// This is a single one-shot call rather than an agent loop, to preserve the
// invariant that the only input is the plan result. Giving it tools would let
// it reach into the repository or the outside world.
package anthropic

import (
	"context"
	"errors"
	"fmt"
	"time"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/nakamasato/tfreview/internal/llm"
)

var ErrPlanTooLarge = errors.New("plan exceeds max_plan_chars")

// ErrResponseTruncated means the Messages API response was cut off by max_tokens.
// The tool_use Input ends up as incomplete JSON that fails ParseAnswers, so this is
// detected ahead of that generic parse error to surface the real cause to the judge.
var ErrResponseTruncated = errors.New("response truncated by max_tokens")

const defaultMaxTokens = 128000

// streamTimeout bounds a single Judge call. Messages.New derives a timeout from
// max_tokens automatically, but Messages.NewStreaming does not, so a stalled
// connection would otherwise hang the caller (and CI) forever.
const streamTimeout = 15 * time.Minute

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
	// An input that's orders of magnitude too large both degrades judgment
	// quality and, once it hits the limit, fails every check with a 400.
	// Returning an error that names the cause as size is more useful to the reviewer.
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

	ctx, cancel := context.WithTimeout(ctx, streamTimeout)
	defer cancel()

	// max_tokens is large enough here that a non-streaming call risks hitting the
	// SDK's HTTP timeout, so stream the response and reassemble the final message.
	stream := p.client.Messages.NewStreaming(ctx, sdk.MessageNewParams{
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
	defer func() { _ = stream.Close() }()

	resp := &sdk.Message{}
	usage := func() llm.Usage {
		return llm.Usage{
			Calls:            1,
			InputTokens:      resp.Usage.InputTokens,
			CacheWriteTokens: resp.Usage.CacheCreationInputTokens,
			CacheReadTokens:  resp.Usage.CacheReadInputTokens,
			OutputTokens:     resp.Usage.OutputTokens,
		}
	}
	for stream.Next() {
		if err := resp.Accumulate(stream.Current()); err != nil {
			// Usage accrues incrementally as the stream progresses (starting at
			// message_start), so report what was billed even on a mid-stream failure.
			return nil, usage(), fmt.Errorf("messages.new (stream): %w", err)
		}
	}
	if err := stream.Err(); err != nil {
		return nil, usage(), fmt.Errorf("messages.new (stream): %w", err)
	}
	if err := checkTruncated(resp, maxTokens, len(req.Checks)); err != nil {
		return nil, usage(), err
	}
	for _, block := range resp.Content {
		if tu, ok := block.AsAny().(sdk.ToolUseBlock); ok {
			answers, err := ParseAnswers(tu.Input)
			return answers, usage(), err
		}
	}
	return nil, usage(), errors.New("response has no tool_use block")
}

// checkTruncated detects that the response was cut off by max_tokens before the
// tool_use Input fails to parse as incomplete JSON. A ParseAnswers error alone
// can't tell the reviewer the cause was a token-budget overrun.
func checkTruncated(resp *sdk.Message, maxTokens int, numChecks int) error {
	if resp.StopReason == sdk.StopReasonMaxTokens {
		return fmt.Errorf("%w: max_tokens=%d, checks=%d", ErrResponseTruncated, maxTokens, numChecks)
	}
	return nil
}
