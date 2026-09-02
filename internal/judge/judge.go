// Package judge は match → LLM → merge → fallback → 集約の順で判定をまとめる。
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
		// 観点はすべて「何が変わったか」を問うので、差分ゼロでは機械判定も LLM 判定も
		// 原理的に成立しない。呼ばずに済ませる。
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
		// ask は match を候補の絞り込みにだけ使う。候補があるときだけ LLM に回し、
		// 答えが得られなければ match の結果に戻す。
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

	// 不完全さは merge 前に確定させる。merge 後は skipped が負けて痕跡が消える。
	for id, vs := range candidates {
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

// 1 target でも答えが欠けたら戻す。merge は max なので、答えが返った側の miss が
// 欠けた側を押し切る。戻したものは決定的な判定なので unevaluated から外す。
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
