// Package eval measures judging quality against labelled plan fixtures. It calls a real
// LLM, so it is opt-in: run it with TFREVIEW_EVAL=1 go test ./eval -v
package eval

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/nakamasato/tfreview/internal/config"
	"github.com/nakamasato/tfreview/internal/judge"
	"github.com/nakamasato/tfreview/internal/llm"
	"github.com/nakamasato/tfreview/internal/llm/claudecli"
	"github.com/nakamasato/tfreview/internal/model"
	"github.com/nakamasato/tfreview/internal/plan"
)

// want lists only the checks expected to be anything other than miss; every check left
// out is expected to miss, which is what makes a false positive fail the case.
var cases = []struct {
	name string
	want map[string]model.VerdictKind
}{
	{"rds-delete", map[string]model.VerdictKind{
		"resource-deletion": model.VerdictHit,
		"stateful-delete":   model.VerdictHit,
		"data-loss":         model.VerdictHit,
	}},
	{"open-ssh", map[string]model.VerdictKind{
		"polp": model.VerdictHit,
	}},
	{"tag-only", nil},
	{"human-iam-revoke", nil},
}

func TestEval(t *testing.T) {
	if os.Getenv("TFREVIEW_EVAL") != "1" {
		t.Skip("set TFREVIEW_EVAL=1 to run (calls the claude CLI)")
	}
	modelName := os.Getenv("TFREVIEW_EVAL_MODEL")
	if modelName == "" {
		modelName = "claude-sonnet-5"
	}
	cfg, err := config.Parse([]byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	provider := claudecli.New(claudecli.Options{Model: modelName, MaxPlanChars: cfg.LLM.MaxPlanChars})

	var ids []string
	for _, ck := range cfg.Checks() {
		ids = append(ids, ck.ID)
	}
	sort.Strings(ids)

	var total, correct int
	var usage llm.Usage
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := plan.Load("cases/" + tc.name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			out, err := judge.Run(context.Background(), judge.Input{
				Config: cfg, Plans: []*plan.Plan{p}, Provider: provider, HeadSHA: "eval",
			})
			if err != nil {
				t.Fatal(err)
			}
			usage.Add(out.Usage)
			// Only disagreements are printed: with 6 checks per case, listing the
			// agreements too makes 20 cases unreadable and hides the failing lines.
			var bad []string
			for _, id := range ids {
				exp := model.VerdictMiss
				if w, ok := tc.want[id]; ok {
					exp = w
				}
				got := out.Verdicts[id]
				total++
				if got.Kind == exp {
					correct++
					continue
				}
				bad = append(bad, fmt.Sprintf("      %-18s want=%-12s got=%-12s %s", id, exp, got.Kind, got.Reason))
			}
			if len(bad) == 0 {
				t.Logf("PASS %s", tc.name)
				return
			}
			t.Errorf("FAIL %s\n%s", tc.name, strings.Join(bad, "\n"))
		})
	}
	t.Logf("accuracy: %d/%d", correct, total)
	t.Logf("tokens:   calls=%d input=%d cache_write=%d cache_read=%d output=%d (total %d)",
		usage.Calls, usage.InputTokens, usage.CacheWriteTokens, usage.CacheReadTokens, usage.OutputTokens,
		usage.InputTokens+usage.CacheWriteTokens+usage.CacheReadTokens+usage.OutputTokens)
	t.Logf("cost:     $%.4f list-price equivalent reported by claude -p (billed to the subscription, not per token)",
		provider.CostUSD())
}
