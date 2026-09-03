package judge

import (
	"testing"

	"github.com/nakamasato/tfreview/internal/config"
	"github.com/nakamasato/tfreview/internal/model"
	"github.com/stretchr/testify/require"
)

func cfg(t *testing.T) *config.Config {
	t.Helper()
	c, err := config.Parse([]byte(`
categories:
  - id: c1
    title: C1
    checks:
      - {id: crit, level: critical, question: q}
      - {id: med, level: medium, question: q}
  - id: c2
    title: C2
    checks:
      - {id: high, level: high, match: {actions: [delete]}}
`))
	require.NoError(t, err)
	return c
}

func TestScoreIsMax(t *testing.T) {
	c := cfg(t)
	vs := map[string]model.Verdict{
		"crit": {Kind: model.VerdictMiss, Source: model.SourceLLM},
		"med":  {Kind: model.VerdictHit, Source: model.SourceLLM},
		"high": {Kind: model.VerdictUnverifiable, Source: model.SourceMachine},
	}
	require.Equal(t, model.LevelHigh, Score(c, vs))
	require.Equal(t, model.LevelMedium, CategoryScore(c.Categories[0], vs))
	require.Equal(t, model.LevelHigh, MachineScore(c, vs))

	vs["crit"] = model.Verdict{Kind: model.VerdictHit, Source: model.SourceLLM}
	require.Equal(t, model.LevelCritical, Score(c, vs))
	require.Equal(t, model.LevelHigh, MachineScore(c, vs))
}

func TestScoreNoneWhenNothingHits(t *testing.T) {
	c := cfg(t)
	require.Equal(t, model.LevelNone, Score(c, map[string]model.Verdict{"crit": {Kind: model.VerdictMiss}}))
	require.Equal(t, model.LevelNone, Score(c, nil))
}

func TestIsIncomplete(t *testing.T) {
	require.False(t, IsIncomplete(map[string]model.Verdict{"a": {Kind: model.VerdictMiss}}, nil))
	require.True(t, IsIncomplete(map[string]model.Verdict{"a": {Kind: model.VerdictSkipped}}, nil))
	require.True(t, IsIncomplete(map[string]model.Verdict{"a": {Kind: model.VerdictMiss}}, map[string]bool{"a": true}))
}
