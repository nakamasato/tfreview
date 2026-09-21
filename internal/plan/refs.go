package plan

import (
	"encoding/json"
	"slices"
	"sort"
	"strings"
)

// The configuration section carries the dependency graph terraform already resolved
// from the .tf files: depends_on, and every attribute's references with module
// expansion, for_each and locals already collapsed. Reading it here rather than
// parsing HCL keeps one answer to "what depends on what" instead of two that can
// disagree.
type configuration struct {
	RootModule moduleConfig `json:"root_module"`
}

type moduleConfig struct {
	Resources []struct {
		Address     string                     `json:"address"`
		DependsOn   []string                   `json:"depends_on"`
		Expressions map[string]json.RawMessage `json:"expressions"`
	} `json:"resources"`
	ModuleCalls map[string]struct {
		Expressions map[string]json.RawMessage `json:"expressions"`
		Module      moduleConfig               `json:"module"`
	} `json:"module_calls"`
}

// graph maps a resource address to the addresses it references. Both sides are
// fully qualified, and only resources that this plan changes are kept: a reference to
// something the plan leaves alone says nothing about the change being judged.
func (c configuration) graph(inPlan map[string]bool) map[string][]string {
	out := map[string][]string{}
	walkModule(c.RootModule, "", nil, inPlan, out)
	return out
}

// vars carries what the parent bound each of this module's variables to, already
// resolved to fully qualified addresses, so a reference to var.x inside the module
// resolves to the parent resource the caller passed in.
func walkModule(m moduleConfig, prefix string, vars map[string][]string, inPlan map[string]bool, out map[string][]string) {
	resolve := func(refs []string) []string {
		var kept []string
		for _, ref := range refs {
			if name, ok := strings.CutPrefix(ref, "var."); ok {
				kept = append(kept, vars[name]...)
				continue
			}
			// A reference reads as both "type.name.attr" and "type.name"; membership in
			// the change set is what picks the address out, so both are offered.
			if addr := prefix + ref; inPlan[addr] {
				kept = append(kept, addr)
			}
		}
		return kept
	}

	for _, r := range m.Resources {
		addr := prefix + r.Address
		if !inPlan[addr] {
			continue
		}
		refs := resolve(append(collectRefs(r.Expressions), r.DependsOn...))
		if len(refs) > 0 {
			out[addr] = dedupe(append(out[addr], refs...))
		}
	}

	for name, call := range m.ModuleCalls {
		childVars := map[string][]string{}
		for v, raw := range call.Expressions {
			// Resolved in the caller's scope, which is where the expression was written.
			if got := resolve(collectRefs(map[string]json.RawMessage{v: raw})); len(got) > 0 {
				childVars[v] = got
			}
		}
		walkModule(call.Module, prefix+"module."+name+".", childVars, inPlan, out)
	}
}

// collectRefs gathers every "references" list in an expression tree. References sit at
// arbitrary depth because a nested block is itself a list of expression maps.
func collectRefs(exprs map[string]json.RawMessage) []string {
	var out []string
	for _, raw := range exprs {
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			continue
		}
		out = append(out, refsIn(v)...)
	}
	return out
}

func refsIn(v any) []string {
	var out []string
	switch t := v.(type) {
	case map[string]any:
		for k, item := range t {
			if k == "references" {
				if list, ok := item.([]any); ok {
					for _, r := range list {
						if s, ok := r.(string); ok {
							out = append(out, s)
						}
					}
				}
				continue
			}
			out = append(out, refsIn(item)...)
		}
	case []any:
		for _, item := range t {
			out = append(out, refsIn(item)...)
		}
	}
	return out
}

func dedupe(in []string) []string {
	sort.Strings(in)
	return slices.Compact(in)
}

// reverse builds referred_by, which is the direction a reviewer asks about: a delete
// matters because of what points at it, not what it points at.
func reverse(g map[string][]string) map[string][]string {
	out := map[string][]string{}
	for from, tos := range g {
		for _, to := range tos {
			out[to] = append(out[to], from)
		}
	}
	for k := range out {
		out[k] = dedupe(out[k])
	}
	return out
}
