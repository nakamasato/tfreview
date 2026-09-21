package eval

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/nakamasato/tfreview/internal/config"
	"github.com/nakamasato/tfreview/internal/llm/jev"
	"github.com/nakamasato/tfreview/internal/model"
	"github.com/nakamasato/tfreview/internal/plan"
)

// TestJevScores prints the score every scored check gives every fixture, which is what
// the thresholds are set from. It fails only on a verdict that disagrees with the
// label, so the printed table stays readable while a regression still breaks the build.
func TestJevScores(t *testing.T) {
	if os.Getenv("TFREVIEW_LIVE") == "" {
		t.Skip("set TFREVIEW_LIVE=1 to call the real API")
	}
	cfg, err := config.Parse([]byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	questions := jev.Questions(cfg.Checks())
	var ids []string
	for id := range questions {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	client := jev.New(jev.Options{APIKey: os.Getenv("TYPESAFE_API_KEY"), Model: cfg.LLM.Jev.Model})
	var tokens int64

	t.Logf("%-18s %-22s %s", "case", "resource", strings.Join(ids, "  "))
	for _, tc := range cases {
		p, err := plan.Load("cases/" + tc.name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		for i, r := range p.Resources {
			resp, err := client.Ask(context.Background(), jev.BuildState(p, i, true, cfg.LLM.Jev.MaxValueChars),
				jev.Focus(questions, i))
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			tokens += resp.Usage.InputTokens

			var cells []string
			for _, id := range ids {
				score := resp.Answers[id].Noul
				got := verdict(score, cfg.LLM.Jev)
				want := model.VerdictMiss
				if w, ok := tc.want[id]; ok {
					want = w
				}
				mark := " "
				if got != want {
					mark = "!"
					t.Errorf("%s / %s: %s scored %.2f -> %s, want %s", tc.name, r.Address, id, score, got, want)
				}
				cells = append(cells, fmt.Sprintf("%.2f%s", score, mark))
			}
			t.Logf("%-18s %-22s %s", tc.name, short(r.Address), strings.Join(cells, "     "))
		}
	}
	t.Logf("input_tokens=%d  cost=$%.5f", tokens, float64(tokens)*0.042/1_000_000)
}

// verdict maps a score onto a verdict. The band between the thresholds is what a later
// stage has to resolve, so it reads as unverifiable until something else decides it.
func verdict(score float64, j config.Jev) model.VerdictKind {
	switch {
	case score >= j.HitThreshold:
		return model.VerdictHit
	case score <= j.MissThreshold:
		return model.VerdictMiss
	}
	return model.VerdictUnverifiable
}

func short(addr string) string {
	if len(addr) <= 22 {
		return addr
	}
	return addr[:21] + "…"
}
