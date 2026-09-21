// Package deepdive takes a second look at the checks a scoring pass left undecided.
//
// The first pass reads one change at a time and answers with a probability, so a score
// in the middle means "this needs looking at" and nothing more. Resolving it takes
// something a scoring judge cannot do: deciding what to examine next, following a
// change to the ones that depend on it, and saying why in words.
//
// Its tools reach only into the plan and back into the scoring judge, never the
// repository or the network, so the invariant that the plan is the only input holds.
package deepdive

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/nakamasato/tfreview/internal/llm"
	"github.com/nakamasato/tfreview/internal/llm/jev"
	"github.com/nakamasato/tfreview/internal/model"
	"github.com/nakamasato/tfreview/internal/plan"
)

const (
	defaultMaxTokens = 8000
	defaultMaxSteps  = 8
	callTimeout      = 5 * time.Minute
)

type Options struct {
	Model     string
	MaxTokens int
	APIKey    string
	// BaseURL overrides the API endpoint. It exists so the tool loop can be driven by a
	// scripted server in tests; leave it empty in a real run.
	BaseURL string
	// MaxSteps bounds the tool loop. A judge that keeps asking without concluding
	// costs more than the verdict is worth, so the loop ends and reports what it has.
	MaxSteps      int
	MaxValueChars int
	Checkpoints   map[string][]model.Checkpoint
	// Scorer lets the judge put its own propositions to the scoring judge. Without one
	// the tool is not offered and it works from the plan alone.
	Scorer *jev.Client
}

type Provider struct {
	opts   Options
	client sdk.Client
}

func New(opts Options) *Provider {
	if opts.MaxTokens == 0 {
		opts.MaxTokens = defaultMaxTokens
	}
	if opts.MaxSteps == 0 {
		opts.MaxSteps = defaultMaxSteps
	}
	var ro []option.RequestOption
	if opts.APIKey != "" {
		ro = append(ro, option.WithAPIKey(opts.APIKey))
	}
	if opts.BaseURL != "" {
		ro = append(ro, option.WithBaseURL(opts.BaseURL))
	}
	return &Provider{opts: opts, client: sdk.NewClient(ro...)}
}

func (p *Provider) Name() string  { return "deepdive" }
func (p *Provider) Model() string { return p.opts.Model }

// Judge examines one check at a time. Keeping the conversations separate costs more
// calls but keeps one inconclusive investigation from colouring the others, and there
// are only ever a handful of undecided checks to look at.
func (p *Provider) Judge(ctx context.Context, req llm.Request) ([]llm.Answer, llm.Usage, error) {
	var out []llm.Answer
	var usage llm.Usage
	for _, ck := range req.Checks {
		a, u := p.examine(ctx, req, ck)
		usage.Add(u)
		out = append(out, a)
	}
	return out, usage, nil
}

func (p *Provider) examine(ctx context.Context, req llm.Request, ck model.Check) (llm.Answer, llm.Usage) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	tools := p.tools()
	messages := []sdk.MessageParam{sdk.NewUserMessage(sdk.NewTextBlock(p.firstMessage(req, ck)))}
	var usage llm.Usage

	for step := 0; step < p.opts.MaxSteps; step++ {
		resp, err := p.client.Messages.New(ctx, sdk.MessageNewParams{
			Model:     sdk.Model(p.opts.Model),
			MaxTokens: int64(p.opts.MaxTokens),
			System: []sdk.TextBlockParam{{
				Text:         system(req.Language),
				CacheControl: sdk.NewCacheControlEphemeralParam(),
			}},
			Messages: messages,
			Tools:    tools,
		})
		if resp != nil {
			usage.Add(llm.Usage{
				Calls:            1,
				InputTokens:      resp.Usage.InputTokens,
				CacheWriteTokens: resp.Usage.CacheCreationInputTokens,
				CacheReadTokens:  resp.Usage.CacheReadInputTokens,
				OutputTokens:     resp.Usage.OutputTokens,
			})
		}
		if err != nil {
			return skipped(ck, "closer look failed: "+err.Error()), usage
		}

		var results []sdk.ContentBlockParamUnion
		for _, block := range resp.Content {
			tu, ok := block.AsAny().(sdk.ToolUseBlock)
			if !ok {
				continue
			}
			if tu.Name == toolReport {
				if a, ok := parseVerdict(ck, tu.Input); ok {
					return a, usage
				}
				results = append(results, sdk.NewToolResultBlock(tu.ID,
					`{"error":"verdict must be one of hit, miss, unverifiable, and reason must not be empty"}`, true))
				continue
			}
			body, err := p.runTool(ctx, req.Plan, tu.Name, tu.Input)
			results = append(results, sdk.NewToolResultBlock(tu.ID, body, err != nil))
		}
		if len(results) == 0 {
			// Nothing was called and no verdict was reported: another turn would only
			// repeat this one, so keep the undecided verdict and say why.
			return unresolved(ck, "closer look ended without a verdict"), usage
		}
		messages = append(messages, resp.ToParam(), sdk.NewUserMessage(results...))
	}
	return unresolved(ck, fmt.Sprintf("closer look ran out of steps after %d", p.opts.MaxSteps)), usage
}

func skipped(ck model.Check, reason string) llm.Answer {
	return llm.Answer{CheckID: ck.ID, Kind: model.VerdictSkipped, Reason: reason}
}

// unresolved keeps the verdict the scoring pass produced. A closer look that reaches no
// conclusion has not turned an undecided check into a miss.
func unresolved(ck model.Check, reason string) llm.Answer {
	return llm.Answer{CheckID: ck.ID, Kind: model.VerdictUnverifiable, Reason: reason}
}

func system(language string) string {
	lang := "in English"
	if language == "ja" {
		lang = "in Japanese (日本語)"
	}
	return fmt.Sprintf(`You settle one proposition about a `+"`terraform plan`"+` result that a first pass scored as undecided.

- Judge only from what the plan shows. Never conclude something the plan does not contain.
- `+"`changed_keys`"+` lists the attributes whose value changed in this plan. `+"`before`"+` is not available, so you can tell that a value changed but not which way it moved, except for a boolean that is now false.
- `+"`unknown_keys`"+` lists attributes whose value is not known until apply; such an attribute showing as null does not mean it is unset.
- `+"`referred_by`"+` names the other changes that point at this one. That is what makes a removal matter.
- Use the tools to look at what you actually need, then call %s exactly once.
- reason: 1-2 sentences %s, naming the resource addresses it rests on.
- Report unverifiable when the plan cannot settle it, and say what is missing.
`, toolReport, lang)
}

func (p *Provider) firstMessage(req llm.Request, ck model.Check) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Target\n\n`%s`\n\n# Proposition (check `%s`)\n\n%s\n", req.Plan.Target, ck.ID, ck.Prose())

	focus := req.Focus[ck.ID]
	if len(focus) > 0 {
		fmt.Fprintf(&sb, "\n# Scored as undecided\n\n%s\n", list(focus))
	}
	fmt.Fprintf(&sb, "\n# Changes in this plan\n\n%s\n", p.summary(req.Plan))
	if cps := p.checkpointsFor(req.Plan, focus); cps != "" {
		fmt.Fprintf(&sb, "\n# What is known about these resource types\n\n%s\n", cps)
	}
	return sb.String()
}

func list(items []string) string {
	var sb strings.Builder
	for _, s := range items {
		sb.WriteString("- " + s + "\n")
	}
	return sb.String()
}

// summary is the plan as addresses and actions only. The detail of any one change is a
// tool call away, which keeps a plan of hundreds of changes from filling the context
// with attributes nobody asked about.
func (p *Provider) summary(pl *plan.Plan) string {
	var sb strings.Builder
	for _, c := range jev.BuildState(pl, -1, false, 0).Changes {
		fmt.Fprintf(&sb, "- `%s` (%s %s)\n", c.Address, c.Action, c.Type)
	}
	return sb.String()
}

// checkpointsFor gathers the configured knowledge for the resource types under
// examination. It goes in the first message rather than behind a tool because it is
// short and always relevant once the shortlist is known.
func (p *Provider) checkpointsFor(pl *plan.Plan, focus []string) string {
	types := map[string]bool{}
	for _, addr := range focus {
		for _, r := range pl.Resources {
			if r.Address == addr {
				types[r.Type] = true
			}
		}
	}
	var sb strings.Builder
	for t := range types {
		for _, cp := range p.opts.Checkpoints[t] {
			fmt.Fprintf(&sb, "- `%s` %s (%s): %s\n", t, cp.ID, cp.Severity, strings.TrimSpace(cp.Guidance))
		}
	}
	return sb.String()
}

func parseVerdict(ck model.Check, input json.RawMessage) (llm.Answer, bool) {
	var in struct {
		Verdict string `json:"verdict"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return llm.Answer{}, false
	}
	k := model.VerdictKind(in.Verdict)
	if k != model.VerdictHit && k != model.VerdictMiss && k != model.VerdictUnverifiable {
		return llm.Answer{}, false
	}
	if strings.TrimSpace(in.Reason) == "" {
		return llm.Answer{}, false
	}
	return llm.Answer{CheckID: ck.ID, Kind: k, Reason: in.Reason}, true
}
