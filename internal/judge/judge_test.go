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
aspects:
  - id: destruction
    title: D
    checks:
      - id: delete-or-replace
        severity: critical
        match: {actions: [delete]}
        verdict_on_match: ask
        instructions: deleted?
      - id: shared
        severity: critical
        match: {targets: [shared]}
        verdict_on_match: unverifiable
  - id: exposure
    title: E
    checks:
      - id: sg-open
        severity: high
        instructions: open?
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

func TestRunRuleAndLLM(t *testing.T) {
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
	require.Equal(t, model.SourceRule, out.Verdicts["shared"].Source)
	require.Equal(t, model.VerdictMiss, out.Verdicts["sg-open"].Kind)
	require.Empty(t, out.Unevaluated)
	require.Equal(t, 2, out.Usage.Calls)
	require.Len(t, out.State.Targets, 2)
	require.False(t, out.Targets[0].Reused)
	// ask checks are passed to the LLM
	require.Contains(t, checkIDs(p.Calls[0].Checks), "delete-or-replace")
	// an already-matched unverifiable is not passed
	require.NotContains(t, checkIDs(p.Calls[0].Checks), "shared")
}

func TestRunAskFallbackWhenOneTargetFails(t *testing.T) {
	// Only dev answers, with a miss; prd fails. With max-merge, dev's miss would
	// win and report "no match" on a PR that does have a delete, so we fall back
	// to match's hit instead.
	p := &flaky{failTarget: "prd", answers: map[string][]llm.Answer{"dev": {{CheckID: "delete-or-replace", Kind: model.VerdictMiss, Reason: "nothing"}, {CheckID: "sg-open", Kind: model.VerdictMiss, Reason: "n"}}}}
	out, err := Run(context.Background(), Input{Config: runCfgParsed(t), Plans: []*plan.Plan{prd(), dev()}, Provider: p, Prev: state.New("", "")})
	require.NoError(t, err)
	v := out.Verdicts["delete-or-replace"]
	require.Equal(t, model.VerdictHit, v.Kind)
	require.Equal(t, model.SourceRule, v.Source)
	require.Contains(t, v.Reason, "using plan facts")
	require.False(t, out.Unevaluated["delete-or-replace"])
	// sg-open was skipped for prd -> judgement is incomplete
	require.True(t, out.Unevaluated["sg-open"])
	require.True(t, IsIncomplete(out.Verdicts, out.Unevaluated))
	// prd, which includes a skipped verdict, is not written to state
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

func TestRunRuleMissNotOverriddenByStaleCachedVerdict(t *testing.T) {
	// prd's current plan has no delete, so match settles it as a rule miss.
	// But state still holds the verdict from a previous run where the LLM said hit
	// (e.g. it had a delete back then). That stale candidate must not override
	// this run's rule miss.
	c := runCfgParsed(t)
	prdNoDelete := &plan.Plan{Target: "prd", Counts: plan.Counts{Add: 1}, Resources: []plan.Resource{{Address: "aws_sqs_queue.q", Type: "aws_sqs_queue", Actions: []string{"create"}}}}
	prev := state.New("old", c.Digest)
	prev.Put("prd", prdNoDelete.Digest(), []model.Verdict{
		{CheckID: "delete-or-replace", Kind: model.VerdictHit, Reason: "stale", Source: model.SourceLLM},
		{CheckID: "sg-open", Kind: model.VerdictMiss, Reason: "stale", Source: model.SourceLLM},
	})
	p := &mock.Provider{Answers: map[string][]llm.Answer{
		"dev": {{CheckID: "sg-open", Kind: model.VerdictMiss, Reason: "no sg"}},
	}}
	out, err := Run(context.Background(), Input{Config: c, Plans: []*plan.Plan{prdNoDelete, dev()}, Provider: p, Prev: prev, HeadSHA: "sha"})
	require.NoError(t, err)
	v := out.Verdicts["delete-or-replace"]
	require.Equal(t, model.SourceRule, v.Source)
	require.Equal(t, model.VerdictMiss, v.Kind)
}

func TestRunPlanTooLargeIsUnverifiable(t *testing.T) {
	p := &mock.Provider{Err: anthropic.ErrPlanTooLarge}
	out, err := Run(context.Background(), Input{Config: runCfgParsed(t), Plans: []*plan.Plan{dev()}, Provider: p, Prev: state.New("", "")})
	require.NoError(t, err)
	require.Equal(t, model.VerdictUnverifiable, out.Verdicts["sg-open"].Kind)
	require.Empty(t, out.Unevaluated)
}

// Truncation is Skipped, not Unverifiable: unlike ErrPlanTooLarge, whether a
// response gets truncated isn't deterministic, so state.Put must not cache it
// (a retry might succeed), and out.Unevaluated must flag it for the reviewer.
func TestRunResponseTruncatedIsSkipped(t *testing.T) {
	p := &mock.Provider{Err: anthropic.ErrResponseTruncated}
	s := state.New("", "")
	out, err := Run(context.Background(), Input{Config: runCfgParsed(t), Plans: []*plan.Plan{dev()}, Provider: p, Prev: s})
	require.NoError(t, err)
	require.Equal(t, model.VerdictSkipped, out.Verdicts["sg-open"].Kind)
	require.True(t, out.Unevaluated["sg-open"])
	_, cached := out.State.Reusable("dev", dev().Digest(), runCfgParsed(t).Digest)
	require.False(t, cached, "a truncated verdict must not be cached")
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

// recorder is a second-pass provider that records what it was asked and answers from a
// fixed table, so the hand-off between the two passes can be checked without an API.
type recorder struct {
	answers map[string]llm.Answer
	got     llm.Request
	calls   int
}

func (r *recorder) Name() string  { return "recorder" }
func (r *recorder) Model() string { return "recorder" }
func (r *recorder) Judge(_ context.Context, req llm.Request) ([]llm.Answer, llm.Usage, error) {
	r.calls++
	r.got = req
	var out []llm.Answer
	for _, ck := range req.Checks {
		if a, ok := r.answers[ck.ID]; ok {
			out = append(out, a)
		}
	}
	return out, llm.Usage{Calls: 1, InputTokens: 7}, nil
}

func deletePlan() []*plan.Plan {
	return []*plan.Plan{{Target: "prd", Counts: plan.Counts{Destroy: 1}, Resources: []plan.Resource{
		{Address: "aws_db_instance.main", Type: "aws_db_instance", Actions: []string{"delete"}},
	}}}
}

func TestRunDeepDiveSettlesUndecided(t *testing.T) {
	first := &mock.Provider{Answers: map[string][]llm.Answer{"prd": {
		{CheckID: "delete-or-replace", Kind: model.VerdictUnverifiable, Reason: "scored 0.42", Resources: []string{"aws_db_instance.main"}, Score: 0.42},
		{CheckID: "sg-open", Kind: model.VerdictMiss, Reason: "nothing"},
	}}}
	deep := &recorder{answers: map[string]llm.Answer{
		"delete-or-replace": {CheckID: "delete-or-replace", Kind: model.VerdictHit, Reason: "the alarm points at it"},
	}}

	out, err := Run(context.Background(), Input{Config: runCfgParsed(t), Plans: deletePlan(), Provider: first, Deep: deep, HeadSHA: "h"})
	require.NoError(t, err)

	// Only the undecided check is looked at again, and it is told which change to open.
	require.Len(t, deep.got.Checks, 1)
	require.Equal(t, "delete-or-replace", deep.got.Checks[0].ID)
	require.Equal(t, map[string][]string{"delete-or-replace": {"aws_db_instance.main"}}, deep.got.Focus)

	v := out.Verdicts["delete-or-replace"]
	require.Equal(t, model.VerdictHit, v.Kind)
	require.Equal(t, "scored 0.42, then on a closer look: the alarm points at it", v.Reason)
	// The first pass's score survives, since it is what the thresholds are tuned on.
	require.Equal(t, 0.42, v.Score)
	require.Equal(t, []string{"aws_db_instance.main"}, v.Resources)
	require.Equal(t, model.VerdictMiss, out.Verdicts["sg-open"].Kind)
	// Both passes are billed: the mock's 1000 plus the second pass's 7.
	require.Equal(t, 1007, int(out.Usage.InputTokens))
}

func TestRunDeepDiveSkippedKeepsTheFirstVerdict(t *testing.T) {
	first := &mock.Provider{Answers: map[string][]llm.Answer{"prd": {
		{CheckID: "delete-or-replace", Kind: model.VerdictUnverifiable, Reason: "scored 0.42", Resources: []string{"aws_db_instance.main"}},
	}}}
	deep := &recorder{answers: map[string]llm.Answer{
		"delete-or-replace": {CheckID: "delete-or-replace", Kind: model.VerdictSkipped, Reason: "closer look failed"},
	}}

	out, err := Run(context.Background(), Input{Config: runCfgParsed(t), Plans: deletePlan(), Provider: first, Deep: deep, HeadSHA: "h"})
	require.NoError(t, err)
	// "needs a closer look" tells a reviewer more than "not evaluated", so a failed
	// second pass must not overwrite it.
	v := out.Verdicts["delete-or-replace"]
	require.Equal(t, model.VerdictUnverifiable, v.Kind)
	require.Equal(t, "scored 0.42", v.Reason)
}

func TestRunDeepDiveSkippedWithNothingToLookAt(t *testing.T) {
	first := &mock.Provider{Answers: map[string][]llm.Answer{"prd": {
		// Unverifiable with no resources means the plan cannot show this at all, which
		// looking harder does not change.
		{CheckID: "delete-or-replace", Kind: model.VerdictUnverifiable, Reason: "the plan cannot show it"},
	}}}
	deep := &recorder{}
	_, err := Run(context.Background(), Input{Config: runCfgParsed(t), Plans: deletePlan(), Provider: first, Deep: deep, HeadSHA: "h"})
	require.NoError(t, err)
	require.Zero(t, deep.calls)
}
