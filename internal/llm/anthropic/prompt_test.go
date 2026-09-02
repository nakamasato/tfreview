package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/nakamasato/tfreview/internal/llm"
	"github.com/nakamasato/tfreview/internal/model"
	"github.com/nakamasato/tfreview/internal/plan"
	"github.com/stretchr/testify/require"
)

func TestBuildSystemLanguage(t *testing.T) {
	require.Contains(t, BuildSystem("en"), "in English")
	require.Contains(t, BuildSystem("ja"), "日本語")
	require.Contains(t, BuildSystem("fr"), "in English")
}

func TestBuildUserContainsTargetPlanAndChecks(t *testing.T) {
	p := &plan.Plan{Target: "prd", Resources: []plan.Resource{{Address: "aws_s3_bucket.x", Type: "aws_s3_bucket", Actions: []string{"delete"}}}}
	req := llm.Request{Plan: p, Checks: []model.Check{{ID: "a", Question: "Q-A?"}, {ID: "b", Question: "Q-B?"}}}
	user := BuildUser(req, PlanJSON(p))
	require.Contains(t, user, "`prd`")
	require.Contains(t, user, `"aws_s3_bucket.x"`)
	require.Contains(t, user, "- a: Q-A?")
	require.Contains(t, user, "- b: Q-B?")
}

func TestPlanJSONIsStable(t *testing.T) {
	p := &plan.Plan{Target: "t", Resources: []plan.Resource{{Address: "x", After: map[string]any{"b": 1, "a": 2}}}}
	require.Equal(t, PlanJSON(p), PlanJSON(p))
	require.Less(t, indexOf(PlanJSON(p), `"a"`), indexOf(PlanJSON(p), `"b"`))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestParseAnswers(t *testing.T) {
	raw := json.RawMessage(`{"verdicts":[{"check_id":"a","verdict":"hit","reason":"r1"},{"check_id":"b","verdict":"nope","reason":"r2"},{"check_id":"c","verdict":"unverifiable","reason":"r3"}]}`)
	got, err := ParseAnswers(raw)
	require.NoError(t, err)
	require.Equal(t, []llm.Answer{{CheckID: "a", Kind: model.VerdictHit, Reason: "r1"}, {CheckID: "c", Kind: model.VerdictUnverifiable, Reason: "r3"}}, got)
}

func TestParseAnswersInvalid(t *testing.T) {
	_, err := ParseAnswers(json.RawMessage(`{`))
	require.Error(t, err)
}
