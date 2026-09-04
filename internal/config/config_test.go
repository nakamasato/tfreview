package config

import (
	"testing"

	"github.com/nakamasato/tfreview/internal/model"
	"github.com/stretchr/testify/require"
)

const minimal = `
language: ja
llm:
  model: claude-opus-5
categories:
  - id: destruction
    title: Destruction
    checks:
      - id: delete-or-replace
        level: critical
        match: { actions: [delete] }
        verdict_on_match: ask
        question: Does it delete?
      - id: unverifiable-thing
        level: high
        match: { targets: [shared] }
        verdict_on_match: unverifiable
      - id: llm-only
        level: medium
        question: Anything odd?
`

func TestParseMinimal(t *testing.T) {
	c, err := Parse([]byte(minimal))
	require.NoError(t, err)
	require.Equal(t, "ja", c.Language)
	require.Equal(t, "anthropic", c.LLM.Provider)
	require.Equal(t, "claude-opus-5", c.LLM.Model)
	require.Equal(t, 100000, c.LLM.MaxPlanChars)
	require.Equal(t, 128000, c.LLM.MaxTokens)
	require.Len(t, c.Categories, 1)
	require.Len(t, c.Checks(), 3)
	require.Regexp(t, `^sha256:[0-9a-f]{64}$`, c.Digest)

	ck, ok := c.Check("delete-or-replace")
	require.True(t, ok)
	require.Equal(t, model.LevelCritical, ck.Level)
	require.Equal(t, model.OnMatchAsk, ck.OnMatch)
	require.Equal(t, []string{"delete"}, ck.Match.Actions)

	ck, _ = c.Check("llm-only")
	require.Equal(t, model.OnMatchHit, ck.OnMatch)
	require.True(t, ck.Match.IsZero())

	cat, ok := c.CategoryOf("llm-only")
	require.True(t, ok)
	require.Equal(t, "destruction", cat.ID)
}

func TestParseUsesBuiltinDefaultWhenNoCategories(t *testing.T) {
	c, err := Parse([]byte("language: en\n"))
	require.NoError(t, err)
	require.NotEmpty(t, c.Categories)
	_, ok := c.Check("delete-or-replace")
	require.True(t, ok)
	_, ok = c.Check("stateful-delete")
	require.True(t, ok)
}

func TestParseEmptyConfig(t *testing.T) {
	c, err := Parse([]byte(""))
	require.NoError(t, err)
	require.Equal(t, "en", c.Language)
	require.NotEmpty(t, c.Categories)
}

func TestDigestChangesWithContent(t *testing.T) {
	a, _ := Parse([]byte(minimal))
	b, _ := Parse([]byte(minimal + "\n# comment\n"))
	require.NotEqual(t, a.Digest, b.Digest)
}

func TestValidationErrors(t *testing.T) {
	cases := map[string]string{
		"bad level":                      "categories: [{id: a, title: A, checks: [{id: x, level: severe, question: q}]}]",
		"bad verdict_on_match":           "categories: [{id: a, title: A, checks: [{id: x, level: high, match: {actions: [delete]}, verdict_on_match: maybe}]}]",
		"unknown match key":              "categories: [{id: a, title: A, checks: [{id: x, level: high, match: {paths: [x]}}]}]",
		"match not list":                 "categories: [{id: a, title: A, checks: [{id: x, level: high, match: {actions: delete}}]}]",
		"no question no match":           "categories: [{id: a, title: A, checks: [{id: x, level: high}]}]",
		"ask without match":              "categories: [{id: a, title: A, checks: [{id: x, level: high, verdict_on_match: ask, question: q}]}]",
		"dup check id":                   "categories: [{id: a, title: A, checks: [{id: x, level: high, question: q}, {id: x, level: high, question: q}]}]",
		"dup category id":                "categories: [{id: a, title: A, checks: [{id: x, level: high, question: q}]}, {id: a, title: B, checks: [{id: y, level: high, question: q}]}]",
		"empty categories":               "categories: []",
		"bad provider":                   "llm: {provider: openai}",
		"question inert on hit":          "categories: [{id: a, title: A, checks: [{id: x, level: high, match: {actions: [delete]}, question: q}]}]",
		"question inert on unverifiable": "categories: [{id: a, title: A, checks: [{id: x, level: high, match: {actions: [delete]}, verdict_on_match: unverifiable, question: q}]}]",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(src))
			require.Error(t, err)
			var ce *Error
			require.ErrorAs(t, err, &ce)
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/.tfreview.yaml")
	require.Error(t, err)
}
