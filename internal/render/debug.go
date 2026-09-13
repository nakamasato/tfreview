package render

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/nakamasato/tfreview/internal/model"
	"github.com/nakamasato/tfreview/internal/plan"
)

const (
	maxAttrValue = 80
	maxAttrLines = 8
)

// Debug renders what went in and what came out, for reading in a terminal while
// tuning checks. Unlike Comment it shows the plan side too: a verdict is only
// debuggable next to the attributes the model was given.
func Debug(r *Result, plans []*plan.Plan, color bool) string {
	p := palette{on: color}
	var b strings.Builder

	// Padding is applied before colouring: an escape sequence counts toward %-12s
	// width and would knock every coloured column out of alignment.
	for _, pl := range plans {
		c := pl.Counts
		fmt.Fprintf(&b, "%s %s  %s\n", p.dim("plan:"), p.bold(pl.Target),
			p.dim(fmt.Sprintf("+%d ~%d -%d ±%d", c.Add, c.Change, c.Destroy, c.Replace)))
		for _, res := range pl.Resources {
			act := strings.Join(res.Actions, "+")
			fmt.Fprintf(&b, "  %s %s\n", p.action(act, fmt.Sprintf("%-8s", act)), p.bold(res.Address))
			lines := attrLines(res)
			for i, line := range lines {
				if i == maxAttrLines {
					fmt.Fprintf(&b, "           %s\n", p.dim(fmt.Sprintf("… +%d more", len(lines)-i)))
					break
				}
				fmt.Fprintf(&b, "           %s\n", p.dim(line))
			}
		}
		b.WriteString("\n")
	}

	label := string(r.Score)
	if r.Incomplete {
		label += " (incomplete)"
	}
	fmt.Fprintf(&b, "%s %s  %s\n", p.dim("review:"), p.severity(r.Score, p.bold(label)), p.dim(r.Label))
	// A miss is the uninteresting answer and there are usually many of them; listing
	// their reasons buries the one or two verdicts worth reading.
	var missed []string
	for _, ck := range sortedChecks(r) {
		if ck.Verdict == model.VerdictMiss {
			missed = append(missed, ck.ID)
			continue
		}
		src := "llm"
		if ck.Source == model.SourceRule {
			src = "rule"
		}
		fmt.Fprintf(&b, "  %s %s %s %s %s\n",
			p.verdict(ck.Verdict, fmt.Sprintf("%-12s", ck.Verdict)),
			p.bold(fmt.Sprintf("%-18s", ck.ID)),
			p.severity(ck.Level, fmt.Sprintf("%-8s", ck.Level)),
			p.dim("["+src+"]"), ck.Reason)
	}
	if len(missed) > 0 {
		fmt.Fprintf(&b, "  %s %s\n", p.dim(fmt.Sprintf("%-12s", "miss")), p.dim(strings.Join(missed, ", ")))
	}

	if r.Usage.Calls > 0 {
		u := r.Usage
		fmt.Fprintf(&b, "\n%s\n", p.dim(fmt.Sprintf("usage: %s · %d calls · in %s / cache write %s / cache read %s / out %s tokens · ≈ $%.4f",
			r.Model, u.Calls, commas(u.InputTokens), commas(u.CacheWriteTokens), commas(u.CacheReadTokens), commas(u.OutputTokens), r.CostUSD)))
	}
	return b.String()
}

// attrLines shows the attributes the check actually sees: changed_keys when the
// plan has them, and otherwise every key of after, which is all a create has.
func attrLines(res plan.Resource) []string {
	if len(res.ChangedKeys) > 0 {
		lines := make([]string, 0, len(res.ChangedKeys))
		for _, k := range res.ChangedKeys {
			lines = append(lines, "~ "+k+" -> "+attrValue(lookup(res.After, k)))
		}
		return append(lines, unknownLine(res)...)
	}
	keys := make([]string, 0, len(res.After))
	for k := range res.After {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, k+"="+attrValue(res.After[k]))
	}
	return append(lines, unknownLine(res)...)
}

func unknownLine(res plan.Resource) []string {
	if len(res.UnknownKeys) == 0 {
		return nil
	}
	return []string{"(known after apply) " + strings.Join(res.UnknownKeys, ", ")}
}

// lookup resolves a dotted changed_keys path such as "tags.Owner" against after.
func lookup(after map[string]any, key string) any {
	var cur any = after
	for _, part := range strings.Split(key, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[part]
	}
	return cur
}

func attrValue(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "?"
	}
	s := string(b)
	if len(s) > maxAttrValue {
		s = s[:maxAttrValue] + "…"
	}
	return s
}

// sortedChecks puts hits first so the reason worth reading is at the top.
func sortedChecks(r *Result) []CheckResult {
	var all []CheckResult
	for _, cat := range r.Categories {
		all = append(all, cat.Checks...)
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Verdict.Rank() != all[j].Verdict.Rank() {
			return all[i].Verdict.Rank() > all[j].Verdict.Rank()
		}
		return all[i].Level.Rank() > all[j].Level.Rank()
	})
	return all
}
