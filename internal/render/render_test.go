package render

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/nakamasato/tfreview/internal/config"
	"github.com/nakamasato/tfreview/internal/judge"
	"github.com/nakamasato/tfreview/internal/llm"
	"github.com/nakamasato/tfreview/internal/model"
	"github.com/nakamasato/tfreview/internal/plan"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "update golden files")

const cfgYAML = `
categories:
  - id: destruction
    title: Destruction / downtime
    checks:
      - {id: delete-or-replace, level: critical, match: {actions: [delete]}, verdict_on_match: ask, question: q}
      - {id: shared, level: critical, match: {targets: [shared]}, verdict_on_match: unverifiable}
  - id: exposure
    title: Permissions / exposure
    checks:
      - {id: sg-open, level: high, question: q}
`

func fixture(t *testing.T, lang string) (*config.Config, *judge.Output, Meta) {
	t.Helper()
	c, err := config.Parse([]byte("language: " + lang + "\n" + cfgYAML))
	require.NoError(t, err)
	out := &judge.Output{
		Verdicts: map[string]model.Verdict{
			"delete-or-replace": {CheckID: "delete-or-replace", Kind: model.VerdictHit, Reason: "aws_db_instance.main is deleted | replaced", Source: model.SourceLLM},
			"shared":            {CheckID: "shared", Kind: model.VerdictMiss, Reason: "no resource matched", Source: model.SourceMachine},
			"sg-open":           {CheckID: "sg-open", Kind: model.VerdictMiss, Reason: "no security group\nchanged", Source: model.SourceLLM},
		},
		Unevaluated: map[string]bool{},
		Targets: []judge.TargetOutcome{
			{Target: "prd", Counts: plan.Counts{Destroy: 1}},
			{Target: "dev", Counts: plan.Counts{Add: 1}, Reused: true},
		},
		Usage: llm.Usage{Calls: 2, InputTokens: 2000, OutputTokens: 200},
	}
	meta := Meta{HeadSHA: "abc1234def5678", JudgedAt: "2026-09-02T00:00:00Z", Repo: "o/r", ConfigPath: ".tfreview.yaml", Model: "claude-opus-5", Pricing: llm.DefaultPricing}
	return c, out, meta
}

func TestBuild(t *testing.T) {
	c, out, meta := fixture(t, "en")
	r := Build(c, out, meta)
	require.Equal(t, model.LevelCritical, r.Score)
	require.Equal(t, model.LevelNone, r.MachineScore)
	require.False(t, r.Incomplete)
	require.Equal(t, "tfreview:critical", r.Label)
	require.Len(t, r.Categories, 2)
	require.Equal(t, 1, r.Categories[0].Hits)
	require.Equal(t, 2, r.Categories[0].Total)
	require.Equal(t, model.LevelCritical, r.Categories[0].Score)
	require.Equal(t, model.LevelNone, r.Categories[1].Score)
	require.InDelta(t, 0.015, r.CostUSD, 1e-9)
}

func TestBuildIncomplete(t *testing.T) {
	c, out, meta := fixture(t, "en")
	out.Unevaluated["sg-open"] = true
	out.Verdicts["sg-open"] = model.Verdict{CheckID: "sg-open", Kind: model.VerdictSkipped, Reason: "LLM judgement failed", Source: model.SourceLLM}
	r := Build(c, out, meta)
	require.True(t, r.Incomplete)
	require.Equal(t, "tfreview:unknown", r.Label)
	require.Equal(t, []string{"sg-open"}, r.Unevaluated)
	body := Comment(r)
	require.Contains(t, body, "incomplete")
	require.Contains(t, body, "sg-open")
	require.Contains(t, body, "img.shields.io/badge/risk-incomplete-0075CA")
	require.NotContains(t, body, "badge/risk-critical")
}

func TestCommentGolden(t *testing.T) {
	for _, lang := range []string{"en", "ja"} {
		t.Run(lang, func(t *testing.T) {
			c, out, meta := fixture(t, lang)
			got := Comment(Build(c, out, meta))
			path := filepath.Join("..", "..", "testdata", "golden", "comment-basic-"+lang+".md")
			if *update {
				require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
			}
			want, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, string(want), got)
		})
	}
}

func TestCommentStructure(t *testing.T) {
	c, out, meta := fixture(t, "en")
	body := Comment(Build(c, out, meta))
	require.True(t, len(body) > 0)
	require.Contains(t, body, Begin)
	require.Contains(t, body, End)
	require.Contains(t, body, "## 🔴 Risk: critical — Destruction / downtime")
	require.Contains(t, body, "img.shields.io/badge/risk-critical-B60205")
	require.Contains(t, body, `<relative-time datetime="2026-09-02T00:00:00Z">`)
	require.Contains(t, body, "https://github.com/o/r/commit/abc1234def5678")
	require.Contains(t, body, "https://github.com/o/r/blob/abc1234def5678/.tfreview.yaml")
	require.Contains(t, body, "🤖 hit")
	require.Contains(t, body, "🔧 miss")
	require.Contains(t, body, `deleted \| replaced`)
	require.NotContains(t, body, "group\nchanged")
	require.Contains(t, body, "| dev | 1 | 0 | 0 | 0 | 0 | reused |")
	require.Contains(t, body, "2 calls")
	require.Contains(t, body, "$0.0150")
}

func TestCommentNoPlansAndNoChanges(t *testing.T) {
	c, _, meta := fixture(t, "en")
	r := Build(c, &judge.Output{NoPlans: true, Verdicts: map[string]model.Verdict{}, Unevaluated: map[string]bool{}}, meta)
	require.Equal(t, "tfreview:none", r.Label)
	require.Contains(t, Comment(r), "No plan was provided")
	require.NotContains(t, Comment(r), "| Category |")

	r = Build(c, &judge.Output{NoChanges: true, Verdicts: map[string]model.Verdict{}, Unevaluated: map[string]bool{}, Targets: []judge.TargetOutcome{{Target: "prd"}}}, meta)
	require.Contains(t, Comment(r), "No changes")
	require.Contains(t, Comment(r), "| prd |")
	require.NotContains(t, Comment(r), "calls")
}

func TestCommentWithoutRepoHasNoLinks(t *testing.T) {
	c, out, meta := fixture(t, "en")
	meta.Repo = ""
	meta.ConfigPath = ""
	body := Comment(Build(c, out, meta))
	require.NotContains(t, body, "https://github.com")
	require.Contains(t, body, "abc1234")
}

func TestStripBlock(t *testing.T) {
	body := "intro\n" + Begin + "\nold\n" + End + "\noutro"
	require.Equal(t, "intro\n\noutro", StripBlock(body))
	require.Equal(t, "plain", StripBlock("plain"))
}

func TestLabelColor(t *testing.T) {
	require.Equal(t, "0E8A16", LabelColor("tfreview:none"))
	require.Equal(t, "B60205", LabelColor("tfreview:critical"))
	require.Equal(t, "0075CA", LabelColor("tfreview:unknown"))
	require.Equal(t, "0075CA", LabelColor("weird"))
}

func TestResultSaveLoad(t *testing.T) {
	c, out, meta := fixture(t, "en")
	r := Build(c, out, meta)
	p := filepath.Join(t.TempDir(), "result.json")
	require.NoError(t, r.Save(p))
	got, err := LoadResult(p)
	require.NoError(t, err)
	require.Equal(t, r, got)
}
