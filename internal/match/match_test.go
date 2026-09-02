package match

import (
	"testing"

	"github.com/nakamasato/tfreview/internal/model"
	"github.com/nakamasato/tfreview/internal/plan"
	"github.com/stretchr/testify/require"
)

func plans() []*plan.Plan {
	return []*plan.Plan{
		{Target: "prd", Resources: []plan.Resource{
			{Address: "aws_db_instance.main", Type: "aws_db_instance", Actions: []string{"delete"}},
			{Address: "aws_sqs_queue.jobs", Type: "aws_sqs_queue", Actions: []string{"create"}},
		}},
		{Target: "shared", Resources: []plan.Resource{
			{Address: "aws_iam_role.ci", Type: "aws_iam_role", Actions: []string{"update"}},
		}},
	}
}

func TestEvaluateNoMatchConfigured(t *testing.T) {
	_, ok := Evaluate(model.Check{ID: "q", Question: "?"}, plans())
	require.False(t, ok)
}

func TestEvaluateActionsHit(t *testing.T) {
	v, ok := Evaluate(model.Check{ID: "del", Match: model.Match{Actions: []string{"delete"}}, OnMatch: model.OnMatchHit}, plans())
	require.True(t, ok)
	require.Equal(t, model.VerdictHit, v.Kind)
	require.Equal(t, model.SourceMachine, v.Source)
	require.Contains(t, v.Reason, "aws_db_instance.main")
	require.Equal(t, "del", v.CheckID)
}

func TestEvaluateActionsAndTypesMustBothMatch(t *testing.T) {
	v, _ := Evaluate(model.Check{ID: "x", Match: model.Match{Actions: []string{"delete"}, Types: []string{"aws_sqs_queue"}}}, plans())
	require.Equal(t, model.VerdictMiss, v.Kind)
	v, _ = Evaluate(model.Check{ID: "x", Match: model.Match{Actions: []string{"delete"}, Types: []string{"aws_db_instance"}}}, plans())
	require.Equal(t, model.VerdictHit, v.Kind)
}

func TestEvaluateTargetsOnly(t *testing.T) {
	v, _ := Evaluate(model.Check{ID: "s", Match: model.Match{Targets: []string{"shared"}}, OnMatch: model.OnMatchUnverifiable}, plans())
	require.Equal(t, model.VerdictUnverifiable, v.Kind)
	require.Contains(t, v.Reason, "shared")
	v, _ = Evaluate(model.Check{ID: "s", Match: model.Match{Targets: []string{"dev"}}, OnMatch: model.OnMatchUnverifiable}, plans())
	require.Equal(t, model.VerdictMiss, v.Kind)
}

func TestEvaluateTargetsScopesResources(t *testing.T) {
	v, _ := Evaluate(model.Check{ID: "s", Match: model.Match{Targets: []string{"shared"}, Actions: []string{"delete"}}}, plans())
	require.Equal(t, model.VerdictMiss, v.Kind)
}

func TestEvaluateAskProducesHit(t *testing.T) {
	v, _ := Evaluate(model.Check{ID: "d", Match: model.Match{Actions: []string{"delete"}}, OnMatch: model.OnMatchAsk}, plans())
	require.Equal(t, model.VerdictHit, v.Kind)
}

func TestCandidates(t *testing.T) {
	rs := Candidates(model.Match{Actions: []string{"create"}}, plans()[0])
	require.Len(t, rs, 1)
	require.Equal(t, "aws_sqs_queue.jobs", rs[0].Address)
	require.Empty(t, Candidates(model.Match{Targets: []string{"dev"}}, plans()[0]))
}

func TestReasonTruncates(t *testing.T) {
	p := &plan.Plan{Target: "t"}
	for i := 0; i < 8; i++ {
		p.Resources = append(p.Resources, plan.Resource{Address: "r" + string(rune('a'+i)), Actions: []string{"delete"}})
	}
	v, _ := Evaluate(model.Check{ID: "d", Match: model.Match{Actions: []string{"delete"}}}, []*plan.Plan{p})
	require.Contains(t, v.Reason, "and 3 more")
}
