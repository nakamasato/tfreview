package config

import (
	"strings"
	"testing"

	"github.com/nakamasato/tfreview/internal/model"
	"github.com/stretchr/testify/require"
)

const minimal = `
language: ja
llm:
  model: claude-opus-5
aspects:
  - id: destruction
    title: Destruction
    checks:
      - id: delete-or-replace
        severity: critical
        match: { actions: [delete] }
        verdict_on_match: ask
        question: Does it delete?
      - id: unverifiable-thing
        severity: high
        match: { targets: [shared] }
        verdict_on_match: unverifiable
      - id: llm-only
        severity: medium
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
	require.Len(t, c.Aspects, 1)
	require.Len(t, c.Checks(), 3)
	require.Regexp(t, `^sha256:[0-9a-f]{64}$`, c.Digest)

	ck, ok := c.Check("delete-or-replace")
	require.True(t, ok)
	require.Equal(t, model.SeverityCritical, ck.Severity)
	require.Equal(t, model.OnMatchAsk, ck.OnMatch)
	require.Equal(t, []string{"delete"}, ck.Match.Actions)

	ck, _ = c.Check("llm-only")
	require.Equal(t, model.OnMatchHit, ck.OnMatch)
	require.True(t, ck.Match.IsZero())

	asp, ok := c.AspectOf("llm-only")
	require.True(t, ok)
	require.Equal(t, "destruction", asp.ID)
}

func TestParseUsesBuiltinDefaultWhenNoAspects(t *testing.T) {
	c, err := Parse([]byte("language: en\n"))
	require.NoError(t, err)
	require.NotEmpty(t, c.Aspects)
	_, ok := c.Check("delete-or-replace")
	require.True(t, ok)
	_, ok = c.Check("stateful-delete")
	require.True(t, ok)
}

func TestParseEmptyConfig(t *testing.T) {
	c, err := Parse([]byte(""))
	require.NoError(t, err)
	require.Equal(t, "en", c.Language)
	require.NotEmpty(t, c.Aspects)
}

func TestDigestChangesWithContent(t *testing.T) {
	a, _ := Parse([]byte(minimal))
	b, _ := Parse([]byte(minimal + "\n# comment\n"))
	require.NotEqual(t, a.Digest, b.Digest)
}

func TestValidationErrors(t *testing.T) {
	cases := map[string]string{
		"bad level":                      "aspects: [{id: a, title: A, checks: [{id: x, severity: severe, question: q}]}]",
		"bad verdict_on_match":           "aspects: [{id: a, title: A, checks: [{id: x, severity: high, match: {actions: [delete]}, verdict_on_match: maybe}]}]",
		"unknown match key":              "aspects: [{id: a, title: A, checks: [{id: x, severity: high, match: {paths: [x]}}]}]",
		"match not list":                 "aspects: [{id: a, title: A, checks: [{id: x, severity: high, match: {actions: delete}}]}]",
		"no question no match":           "aspects: [{id: a, title: A, checks: [{id: x, severity: high}]}]",
		"ask without match":              "aspects: [{id: a, title: A, checks: [{id: x, severity: high, verdict_on_match: ask, question: q}]}]",
		"dup check id":                   "aspects: [{id: a, title: A, checks: [{id: x, severity: high, question: q}, {id: x, severity: high, question: q}]}]",
		"dup aspect id":                  "aspects: [{id: a, title: A, checks: [{id: x, severity: high, question: q}]}, {id: a, title: B, checks: [{id: y, severity: high, question: q}]}]",
		"empty aspects":                  "aspects: []",
		"bad provider":                   "llm: {provider: openai}",
		"question inert on hit":          "aspects: [{id: a, title: A, checks: [{id: x, severity: high, match: {actions: [delete]}, question: q}]}]",
		"question inert on unverifiable": "aspects: [{id: a, title: A, checks: [{id: x, severity: high, match: {actions: [delete]}, verdict_on_match: unverifiable, question: q}]}]",
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

const checkpointYAML = `
aspects:
  - id: data-loss
    title: Data loss
    checks:
      - id: guard-relaxed
        severity: critical
        question: is a guard relaxed?
checkpoints_for_resource:
  aws_db_instance:
    - id: rds-deletion-protection
      aspect: data-loss
      severity: critical
      references:
        - https://example.com/docs
      guidance: |
        Has deletion_protection moved from true to false?
`

func TestParseCheckpoints(t *testing.T) {
	c, err := Parse([]byte(checkpointYAML))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	cps := c.CheckpointsFor("aws_db_instance")
	if len(cps) != 1 {
		t.Fatalf("CheckpointsFor returned %d checkpoints, want 1", len(cps))
	}
	if cps[0].ID != "rds-deletion-protection" || cps[0].Aspect != "data-loss" {
		t.Errorf("checkpoint = %+v", cps[0])
	}
	if cps[0].Severity != model.SeverityCritical {
		t.Errorf("Severity = %q, want critical", cps[0].Severity)
	}
	if len(c.CheckpointsFor("aws_s3_bucket")) != 0 {
		t.Error("CheckpointsFor returned checkpoints for an unrelated type")
	}
}

func TestParseRejects(t *testing.T) {
	cases := map[string]struct{ yaml, want string }{
		"legacy categories": {
			yaml: "categories:\n  - id: x\n",
			want: "aspects",
		},
		"legacy level": {
			yaml: "aspects:\n  - id: a\n    checks:\n      - id: c\n        level: high\n        question: q\n",
			want: "severity",
		},
		"checkpoint without aspect": {
			yaml: "aspects:\n  - id: a\ncheckpoints_for_resource:\n  aws_db_instance:\n    - id: x\n      severity: high\n      guidance: g\n",
			want: "aspect is required",
		},
		"checkpoint unknown aspect": {
			yaml: "aspects:\n  - id: a\ncheckpoints_for_resource:\n  aws_db_instance:\n    - id: x\n      aspect: nope\n      severity: high\n      guidance: g\n",
			want: `unknown aspect "nope"`,
		},
		"checkpoint without guidance": {
			yaml: "aspects:\n  - id: a\ncheckpoints_for_resource:\n  aws_db_instance:\n    - id: x\n      aspect: a\n      severity: high\n",
			want: "guidance must not be empty",
		},
		"checkpoint without severity": {
			yaml: "aspects:\n  - id: a\ncheckpoints_for_resource:\n  aws_db_instance:\n    - id: x\n      aspect: a\n      guidance: g\n",
			want: "severity must be medium, high or critical",
		},
		"checkpoint severity none": {
			yaml: "aspects:\n  - id: a\ncheckpoints_for_resource:\n  aws_db_instance:\n    - id: x\n      aspect: a\n      severity: none\n      guidance: g\n",
			want: "severity must be medium, high or critical",
		},
		"duplicate id across check and checkpoint": {
			yaml: "aspects:\n  - id: a\n    checks:\n      - id: dup\n        severity: high\n        question: q\ncheckpoints_for_resource:\n  aws_db_instance:\n    - id: dup\n      aspect: a\n      severity: high\n      guidance: g\n",
			want: "duplicated",
		},
		"empty resource type": {
			yaml: "aspects:\n  - id: a\ncheckpoints_for_resource:\n  \"\":\n    - id: x\n      aspect: a\n      severity: high\n      guidance: g\n",
			want: "resource type must not be empty",
		},
		"unknown requires": {
			yaml: "aspects:\n  - id: a\n    checks:\n      - id: c\n        severity: high\n        requires: [repo]\n        question: q\n",
			want: "unknown requires",
		},
		"non-url reference": {
			yaml: "aspects:\n  - id: a\ncheckpoints_for_resource:\n  aws_db_instance:\n    - id: x\n      aspect: a\n      severity: high\n      guidance: g\n      references: [not-a-url]\n",
			want: "must be an http(s) URL",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(tc.yaml))
			if err == nil {
				t.Fatal("Parse returned no error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestParseRequires(t *testing.T) {
	c, err := Parse([]byte("aspects:\n  - id: a\n    checks:\n      - id: c\n        severity: high\n        requires: [diff, pr]\n        question: q\n"))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	ck, _ := c.Check("c")
	if !model.HasRequirement(ck.Requires, model.RequiresDiff) || !model.HasRequirement(ck.Requires, model.RequiresPR) {
		t.Errorf("Requires = %v", ck.Requires)
	}
}
