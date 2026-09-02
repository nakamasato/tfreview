package judge

import (
	"context"
	"errors"
	"testing"

	"github.com/nakamasato/tfreview/internal/config"
	"github.com/nakamasato/tfreview/internal/llm"
	"github.com/nakamasato/tfreview/internal/llm/anthropic"
	"github.com/nakamasato/tfreview/internal/llm/mock"
	"github.com/nakamasato/tfreview/internal/model"
	"github.com/nakamasato/tfreview/internal/plan"
	"github.com/nakamasato/tfreview/internal/state"
	"github.com/stretchr/testify/require"
)

const runCfg = `
categories:
  - id: destruction
    title: D
    checks:
      - id: delete-or-replace
        level: critical
        match: {actions: [delete]}
        verdict_on_match: ask
        question: deleted?
      - id: shared
        level: critical
        match: {targets: [shared]}
        verdict_on_match: unverifiable
  - id: exposure
    title: E
    checks:
      - id: sg-open
        level: high
        question: open?
`

func runCfgParsed(t *testing.T) *config.Config {
	t.Helper()
	c, err := config.Parse([]byte(runCfg))
	require.NoError(t, err)
	return c
}

func prd() *plan.Plan {
	return &plan.Plan{Target: "prd", Counts: plan.Counts{Destroy: 1}, Resources: []plan.Resource{{Address: "aws_db_instance.main", Type: "aws_db_instance", Actions: []string{"delete"}}}}
}

func dev() *plan.Plan {
	return &plan.Plan{Target: "dev", Counts: plan.Counts{Add: 1}, Resources: []plan.Resource{{Address: "aws_sqs_queue.q", Type: "aws_sqs_queue", Actions: []string{"create"}}}}
}

func TestRunNoPlans(t *testing.T) {
	out, err := Run(context.Background(), Input{Config: runCfgParsed(t), Provider: &mock.Provider{}, HeadSHA: "sha"})
	require.NoError(t, err)
	require.True(t, out.NoPlans)
	require.Equal(t, "sha", out.State.HeadSHA)
}

func TestRunNoChangesSkipsLLM(t *testing.T) {
	p := &mock.Provider{}
	out, err := Run(context.Background(), Input{Config: runCfgParsed(t), Plans: []*plan.Plan{{Target: "prd"}}, Provider: p})
	require.NoError(t, err)
	require.True(t, out.NoChanges)
	require.Empty(t, p.Calls)
}

func TestRunMachineAndLLM(t *testing.T) {
	p := &mock.Provider{Answers: map[string][]llm.Answer{
		"prd": {{CheckID: "delete-or-replace", Kind: model.VerdictHit, Reason: "db deleted"}, {CheckID: "sg-open", Kind: model.VerdictMiss, Reason: "no sg"}},
		"dev": {{CheckID: "delete-or-replace", Kind: model.VerdictMiss, Reason: "nothing"}, {CheckID: "sg-open", Kind: model.VerdictMiss, Reason: "no sg"}},
	}}
	out, err := Run(context.Background(), Input{Config: runCfgParsed(t), Plans: []*plan.Plan{prd(), dev()}, Provider: p, Prev: state.New("", ""), HeadSHA: "sha"})
	require.NoError(t, err)
	require.Len(t, p.Calls, 2)
	require.Equal(t, model.VerdictHit, out.Verdicts["delete-or-replace"].Kind)
	require.Equal(t, model.SourceLLM, out.Verdicts["delete-or-replace"].Source)
	require.Equal(t, model.VerdictMiss, out.Verdicts["shared"].Kind)
	require.Equal(t, model.SourceMachine, out.Verdicts["shared"].Source)
	require.Equal(t, model.VerdictMiss, out.Verdicts["sg-open"].Kind)
	require.Empty(t, out.Unevaluated)
	require.Equal(t, 2, out.Usage.Calls)
	require.Len(t, out.State.Targets, 2)
	require.False(t, out.Targets[0].Reused)
	// ask のチェックは LLM に渡される
	require.Contains(t, checkIDs(p.Calls[0].Checks), "delete-or-replace")
	// match 済みの unverifiable は渡されない
	require.NotContains(t, checkIDs(p.Calls[0].Checks), "shared")
}

func TestRunAskFallbackWhenOneTargetFails(t *testing.T) {
	// dev だけ答えが返り miss。prd は失敗。max merge だと dev の miss が勝って
	// 「削除が出ている PR が該当なし」になるので、match の hit に戻す。
	p := &flaky{failTarget: "prd", answers: map[string][]llm.Answer{"dev": {{CheckID: "delete-or-replace", Kind: model.VerdictMiss, Reason: "nothing"}, {CheckID: "sg-open", Kind: model.VerdictMiss, Reason: "n"}}}}
	out, err := Run(context.Background(), Input{Config: runCfgParsed(t), Plans: []*plan.Plan{prd(), dev()}, Provider: p, Prev: state.New("", "")})
	require.NoError(t, err)
	v := out.Verdicts["delete-or-replace"]
	require.Equal(t, model.VerdictHit, v.Kind)
	require.Equal(t, model.SourceMachine, v.Source)
	require.Contains(t, v.Reason, "using plan facts")
	require.False(t, out.Unevaluated["delete-or-replace"])
	// sg-open は prd で skipped → 判定不完全
	require.True(t, out.Unevaluated["sg-open"])
	require.True(t, IsIncomplete(out.Verdicts, out.Unevaluated))
	// skipped を含む prd は state に書かれない
	_, ok := out.State.Targets["prd"]
	require.False(t, ok)
	_, ok = out.State.Targets["dev"]
	require.True(t, ok)
}

func TestRunReusesState(t *testing.T) {
	c := runCfgParsed(t)
	prev := state.New("old", c.Digest)
	prev.Put("prd", prd().Digest(), []model.Verdict{
		{CheckID: "delete-or-replace", Kind: model.VerdictMiss, Reason: "cached", Source: model.SourceLLM},
		{CheckID: "sg-open", Kind: model.VerdictMiss, Reason: "cached", Source: model.SourceLLM},
	})
	p := &mock.Provider{}
	out, err := Run(context.Background(), Input{Config: c, Plans: []*plan.Plan{prd()}, Provider: p, Prev: prev})
	require.NoError(t, err)
	require.Empty(t, p.Calls)
	require.True(t, out.Targets[0].Reused)
	require.Equal(t, "cached", out.Verdicts["sg-open"].Reason)
	require.Equal(t, 0, out.Usage.Calls)
}

func TestRunPlanTooLargeIsUnverifiable(t *testing.T) {
	p := &mock.Provider{Err: anthropic.ErrPlanTooLarge}
	out, err := Run(context.Background(), Input{Config: runCfgParsed(t), Plans: []*plan.Plan{dev()}, Provider: p, Prev: state.New("", "")})
	require.NoError(t, err)
	require.Equal(t, model.VerdictUnverifiable, out.Verdicts["sg-open"].Kind)
	require.Empty(t, out.Unevaluated)
}

func TestRunMissingAnswerIsSkipped(t *testing.T) {
	p := &mock.Provider{Answers: map[string][]llm.Answer{"dev": {}}}
	out, err := Run(context.Background(), Input{Config: runCfgParsed(t), Plans: []*plan.Plan{dev()}, Provider: p, Prev: state.New("", "")})
	require.NoError(t, err)
	require.Equal(t, model.VerdictSkipped, out.Verdicts["sg-open"].Kind)
	require.True(t, out.Unevaluated["sg-open"])
}

type flaky struct {
	failTarget string
	answers    map[string][]llm.Answer
}

func (f *flaky) Name() string  { return "flaky" }
func (f *flaky) Model() string { return "m" }
func (f *flaky) Judge(_ context.Context, req llm.Request) ([]llm.Answer, llm.Usage, error) {
	if req.Plan.Target == f.failTarget {
		return nil, llm.Usage{}, errors.New("api down")
	}
	return f.answers[req.Plan.Target], llm.Usage{Calls: 1}, nil
}

func checkIDs(cs []model.Check) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}
