// Package match は plan の事実だけで決まる判定を行う。LLM は関与しない。
package match

import (
	"fmt"
	"slices"
	"strings"

	"github.com/nakamasato/tfreview/internal/model"
	"github.com/nakamasato/tfreview/internal/plan"
)

const maxListed = 5

func Evaluate(check model.Check, plans []*plan.Plan) (model.Verdict, bool) {
	if check.Match.IsZero() {
		return model.Verdict{}, false
	}
	v := model.Verdict{CheckID: check.ID, Source: model.SourceMachine, Kind: model.VerdictMiss, Reason: "no resource matched"}
	targetsOnly := len(check.Match.Actions) == 0 && len(check.Match.Types) == 0

	var matched []string
	var matchedTargets []string
	for _, p := range plans {
		if !targetAllowed(check.Match, p.Target) {
			continue
		}
		if targetsOnly {
			if p.HasChanges() {
				matchedTargets = append(matchedTargets, p.Target)
			}
			continue
		}
		for _, r := range Candidates(check.Match, p) {
			matched = append(matched, r.Address)
		}
	}
	if len(matched) == 0 && len(matchedTargets) == 0 {
		return v, true
	}

	v.Kind = model.VerdictHit
	if check.OnMatch == model.OnMatchUnverifiable {
		v.Kind = model.VerdictUnverifiable
	}
	if targetsOnly {
		v.Reason = fmt.Sprintf("target %s matched", strings.Join(matchedTargets, ", "))
	} else {
		v.Reason = fmt.Sprintf("%d resource(s) matched: %s", len(matched), listAddresses(matched))
	}
	return v, true
}

func Candidates(m model.Match, p *plan.Plan) []plan.Resource {
	if !targetAllowed(m, p.Target) {
		return nil
	}
	var out []plan.Resource
	for _, r := range p.Resources {
		if len(m.Types) > 0 && !slices.Contains(m.Types, r.Type) {
			continue
		}
		if len(m.Actions) > 0 && !anyIn(m.Actions, r.Actions) {
			continue
		}
		out = append(out, r)
	}
	return out
}

func targetAllowed(m model.Match, target string) bool {
	return len(m.Targets) == 0 || slices.Contains(m.Targets, target)
}

func anyIn(want, have []string) bool {
	for _, h := range have {
		if slices.Contains(want, h) {
			return true
		}
	}
	return false
}

func listAddresses(addrs []string) string {
	if len(addrs) <= maxListed {
		return strings.Join(addrs, ", ")
	}
	return fmt.Sprintf("%s …and %d more", strings.Join(addrs[:maxListed], ", "), len(addrs)-maxListed)
}
