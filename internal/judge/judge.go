// Package judge assembles verdicts through the pipeline: match -> LLM -> merge -> fallback -> aggregate.
package judge

import (
	"context"
	"errors"
	"fmt"

	"github.com/nakamasato/tfreview/internal/config"
	"github.com/nakamasato/tfreview/internal/llm"
	"github.com/nakamasato/tfreview/internal/llm/anthropic"
	"github.com/nakamasato/tfreview/internal/match"
	"github.com/nakamasato/tfreview/internal/model"
	"github.com/nakamasato/tfreview/internal/plan"
	"github.com/nakamasato/tfreview/internal/state"
)

type Input struct {
	Config   *config.Config
	Plans    []*plan.Plan
	Provider llm.Provider
	// Deep takes a second look at the checks Provider left undecided. Nil skips it,
	// and those checks stay unverifiable.
	Deep    llm.Provider
	Prev    *state.State
	HeadSHA string
}

type TargetOutcome struct {
	Target string
	Counts plan.Counts
	Reused bool
}

type Output struct {
	Verdicts    map[string]model.Verdict
	Unevaluated map[string]bool
	Targets     []TargetOutcome
	Usage       llm.Usage
	// DeepUsage is the second pass, kept apart because it is billed by a different
	// provider at rates that differ by orders of magnitude.
	DeepUsage llm.Usage
	State     *state.State
	NoPlans   bool
	NoChanges bool
}

func Run(ctx context.Context, in Input) (*Output, error) {
	cfg := in.Config
	out := &Output{
		Verdicts:    map[string]model.Verdict{},
		Unevaluated: map[string]bool{},
		State:       state.New(in.HeadSHA, cfg.Digest),
	}
	if in.Prev == nil {
		in.Prev = state.New("", "")
	}
	for _, p := range in.Plans {
		out.Targets = append(out.Targets, TargetOutcome{Target: p.Target, Counts: p.Counts})
	}
	if len(in.Plans) == 0 {
		out.NoPlans = true
		return out, nil
	}
	if !anyChanges(in.Plans) {
		// Every check asks "what changed", so with zero diff neither a rule nor an LLM
		// judgement can produce anything meaningful — skip calling them at all.
		out.NoChanges = true
		return out, nil
	}

	checks := cfg.Checks()
	ruleDecided := map[string]bool{}
	askFallback := map[string]model.Verdict{}
	for _, ck := range checks {
		v, ok := match.Evaluate(ck, in.Plans)
		if !ok {
			continue
		}
		// For ask checks, match only narrows candidates. We only send it to the LLM
		// when there's a candidate, and fall back to the match result if no answer comes back.
		if ck.OnMatch == model.OnMatchAsk && v.Kind != model.VerdictMiss {
			askFallback[ck.ID] = v
			continue
		}
		out.Verdicts[ck.ID] = v
		ruleDecided[ck.ID] = true
	}

	var llmChecks []model.Check
	for _, ck := range checks {
		if !ruleDecided[ck.ID] && ck.Instructions != "" {
			llmChecks = append(llmChecks, ck)
		}
	}

	candidates := map[string][]model.Verdict{}
	for i, p := range in.Plans {
		digest := p.Digest()
		var vs []model.Verdict
		if cached, ok := in.Prev.Reusable(p.Target, digest, cfg.Digest); ok {
			out.Targets[i].Reused = true
			vs = cached
		} else if len(llmChecks) > 0 {
			var usage llm.Usage
			vs, usage = judgeTarget(ctx, in.Provider, llm.Request{Plan: p, Checks: llmChecks, Language: cfg.Language})
			out.Usage.Add(usage)
			if in.Deep != nil {
				var deepUsage llm.Usage
				vs, deepUsage = deepen(ctx, in.Deep, p, llmChecks, vs, cfg.Language)
				out.DeepUsage.Add(deepUsage)
			}
		}
		out.State.Put(p.Target, digest, vs)
		for _, v := range vs {
			candidates[v.CheckID] = append(candidates[v.CheckID], v)
		}
	}

	// Record incompleteness before merging: after merge, a skipped verdict loses to
	// whatever it's merged with and leaves no trace.
	for id, vs := range candidates {
		if ruleDecided[id] {
			// A target reused from state may still carry a stale verdict for a check
			// that used to require an LLM judgement but this time was settled by
			// match alone. Don't let that stale candidate override match's decisive
			// verdict from this run.
			continue
		}
		for _, v := range vs {
			if v.Kind == model.VerdictSkipped {
				out.Unevaluated[id] = true
			}
		}
		out.Verdicts[id] = Merge(vs)
	}

	applyAskFallback(askFallback, out.Verdicts, out.Unevaluated)
	return out, nil
}

func judgeTarget(ctx context.Context, provider llm.Provider, req llm.Request) ([]model.Verdict, llm.Usage) {
	answers, usage, err := provider.Judge(ctx, req)
	byID := map[string]llm.Answer{}
	for _, a := range answers {
		byID[a.CheckID] = a
	}
	var out []model.Verdict
	for _, ck := range req.Checks {
		v := model.Verdict{CheckID: ck.ID, Source: model.SourceLLM}
		switch {
		case err != nil && errors.Is(err, anthropic.ErrPlanTooLarge):
			v.Kind = model.VerdictUnverifiable
			v.Reason = "plan too large for LLM judgement: " + err.Error()
		case err != nil && errors.Is(err, anthropic.ErrResponseTruncated):
			// Unlike ErrPlanTooLarge, truncation isn't deterministic (response length
			// varies run to run), so this must stay Skipped rather than Unverifiable:
			// state.Put excludes Skipped from the cache, letting a retry re-judge it
			// instead of freezing a one-off truncation for the plan's lifetime.
			v.Kind = model.VerdictSkipped
			v.Reason = "LLM response was truncated by max_tokens; split the config or reduce the number of checks: " + err.Error()
		case err != nil:
			v.Kind = model.VerdictSkipped
			v.Reason = "LLM judgement failed: " + err.Error()
		default:
			a, ok := byID[ck.ID]
			if !ok {
				v.Kind = model.VerdictSkipped
				v.Reason = "no answer returned"
			} else {
				v.Kind = a.Kind
				v.Reason = a.Reason
				v.Resources = a.Resources
				v.Score = a.Score
			}
		}
		out = append(out, v)
	}
	return out, usage
}

// Fall back if even one target is missing an answer: since merge takes the max,
// a miss from a target that did answer would otherwise override the missing one.
// The fallback is a decisive verdict, so remove it from unevaluated.
func applyAskFallback(fallback map[string]model.Verdict, verdicts map[string]model.Verdict, unevaluated map[string]bool) {
	for id, fb := range fallback {
		v, ok := verdicts[id]
		if ok && v.Kind != model.VerdictSkipped && !unevaluated[id] {
			continue
		}
		fb.Reason = "LLM answer unavailable; using plan facts: " + fb.Reason
		verdicts[id] = fb
		delete(unevaluated, id)
	}
}

func anyChanges(plans []*plan.Plan) bool {
	for _, p := range plans {
		if p.HasChanges() {
			return true
		}
	}
	return false
}

// deepen re-judges only what the first pass could not settle, and only while it named
// the changes to look at. An unverifiable verdict with nothing to point at is a
// statement that the plan cannot show this, which a closer look will not change.
func deepen(ctx context.Context, deep llm.Provider, p *plan.Plan, checks []model.Check, vs []model.Verdict, language string) ([]model.Verdict, llm.Usage) {
	focus := map[string][]string{}
	for _, v := range vs {
		if v.Kind == model.VerdictUnverifiable && len(v.Resources) > 0 {
			focus[v.CheckID] = v.Resources
		}
	}
	if len(focus) == 0 {
		return vs, llm.Usage{}
	}
	var undecided []model.Check
	for _, ck := range checks {
		if _, ok := focus[ck.ID]; ok {
			undecided = append(undecided, ck)
		}
	}
	deeper, usage := judgeTarget(ctx, deep, llm.Request{Plan: p, Checks: undecided, Language: language, Focus: focus})
	byID := map[string]model.Verdict{}
	for _, v := range deeper {
		byID[v.CheckID] = v
	}
	out := make([]model.Verdict, 0, len(vs))
	for _, v := range vs {
		// A closer look that failed outright leaves the first pass's verdict in place:
		// "needs a closer look" is more use to a reviewer than "not evaluated".
		if d, ok := byID[v.CheckID]; ok && d.Kind != model.VerdictSkipped {
			d.Resources = v.Resources
			d.Score = v.Score
			// Keep the trail: a reviewer reading only the agent's conclusion cannot tell it
			// was a second opinion on something the first pass could not settle.
			d.Reason = fmt.Sprintf("scored %.2f, then on a closer look: %s", v.Score, d.Reason)
			out = append(out, d)
			continue
		}
		out = append(out, v)
	}
	return out, usage
}
