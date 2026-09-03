// Package judge assembles verdicts through the pipeline: match -> LLM -> merge -> fallback -> aggregate.
package judge

import (
	"context"
	"errors"

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
	Prev     *state.State
	HeadSHA  string
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
	State       *state.State
	NoPlans     bool
	NoChanges   bool
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
		// Every check asks "what changed", so with zero diff neither machine nor LLM
		// judgement can produce anything meaningful — skip calling them at all.
		out.NoChanges = true
		return out, nil
	}

	checks := cfg.Checks()
	machine := map[string]bool{}
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
		machine[ck.ID] = true
	}

	var llmChecks []model.Check
	for _, ck := range checks {
		if !machine[ck.ID] && ck.Question != "" {
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
		}
		out.State.Put(p.Target, digest, vs)
		for _, v := range vs {
			candidates[v.CheckID] = append(candidates[v.CheckID], v)
		}
	}

	// Record incompleteness before merging: after merge, a skipped verdict loses to
	// whatever it's merged with and leaves no trace.
	for id, vs := range candidates {
		if machine[id] {
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
