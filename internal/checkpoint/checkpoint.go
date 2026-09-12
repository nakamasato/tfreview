// Package checkpoint selects the per-resource-type knowledge that applies to a plan.
// Nothing here calls the LLM: this is the step that keeps prompt size proportional to
// the resources a plan touches rather than to the size of the config.
package checkpoint

import (
	"sort"

	"github.com/nakamasato/tfreview/internal/model"
	"github.com/nakamasato/tfreview/internal/plan"
)

type Group struct {
	ResourceType string
	Addresses    []string
	Checkpoints  []model.Checkpoint
}

type Inputs struct {
	Diff bool
	PR   bool
}

func Satisfied(reqs []string, in Inputs) bool {
	if model.HasRequirement(reqs, model.RequiresDiff) && !in.Diff {
		return false
	}
	if model.HasRequirement(reqs, model.RequiresPR) && !in.PR {
		return false
	}
	return true
}

func Select(cps map[string][]model.Checkpoint, p *plan.Plan, in Inputs) []Group {
	byType := addressesByType(cps, p)
	types := make([]string, 0, len(byType))
	for t := range byType {
		types = append(types, t)
	}
	sort.Strings(types)

	var out []Group
	for _, t := range types {
		var kept []model.Checkpoint
		for _, cp := range cps[t] {
			if Satisfied(cp.Requires, in) {
				kept = append(kept, cp)
			}
		}
		if len(kept) == 0 {
			continue
		}
		out = append(out, Group{ResourceType: t, Addresses: byType[t], Checkpoints: kept})
	}
	return out
}

func Held(cps map[string][]model.Checkpoint, p *plan.Plan, in Inputs) []model.Checkpoint {
	byType := addressesByType(cps, p)
	types := make([]string, 0, len(byType))
	for t := range byType {
		types = append(types, t)
	}
	sort.Strings(types)

	var out []model.Checkpoint
	for _, t := range types {
		for _, cp := range cps[t] {
			if !Satisfied(cp.Requires, in) {
				out = append(out, cp)
			}
		}
	}
	return out
}

func addressesByType(cps map[string][]model.Checkpoint, p *plan.Plan) map[string][]string {
	byType := map[string][]string{}
	if p == nil {
		return byType
	}
	for _, r := range p.Resources {
		if len(cps[r.Type]) == 0 {
			continue
		}
		byType[r.Type] = append(byType[r.Type], r.Address)
	}
	return byType
}
