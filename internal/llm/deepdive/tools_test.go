package deepdive

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nakamasato/tfreview/internal/llm"
	"github.com/nakamasato/tfreview/internal/llm/jev"
	"github.com/nakamasato/tfreview/internal/model"
	"github.com/nakamasato/tfreview/internal/plan"
	"github.com/stretchr/testify/require"
)

func testPlan() *plan.Plan {
	return &plan.Plan{Target: "prd", Counts: plan.Counts{Destroy: 1, Add: 1}, Resources: []plan.Resource{
		{
			Address: "aws_db_instance.orders", Type: "aws_db_instance", Actions: []string{"delete"},
			ReferredBy: []string{"aws_cloudwatch_metric_alarm.db"},
			After:      map[string]any{"identifier": "orders", "policy": strings.Repeat("x", 40)},
		},
		{
			Address: "aws_cloudwatch_metric_alarm.db", Type: "aws_cloudwatch_metric_alarm", Actions: []string{"create"},
			Refs: []string{"aws_db_instance.orders"},
		},
	}}
}

func TestGetChange(t *testing.T) {
	p := New(Options{MaxValueChars: 10})
	body, err := p.runTool(context.Background(), testPlan(), toolGetChange, json.RawMessage(`{"address":"aws_db_instance.orders"}`))
	require.NoError(t, err)

	var got jev.Change
	require.NoError(t, json.Unmarshal([]byte(body), &got))
	require.Equal(t, "destroy", got.Action)
	// referred_by is the reason a removal matters, so it has to reach the judge.
	require.Equal(t, []string{"aws_cloudwatch_metric_alarm.db"}, got.ReferredBy)
	require.Equal(t, "orders", got.After["identifier"])
	// The same trimming the scoring pass applied, so a value reads the same both times.
	require.Contains(t, got.After["policy"], "(40 chars total)")
}

func TestGetChangeUnknownAddress(t *testing.T) {
	p := New(Options{})
	body, err := p.runTool(context.Background(), testPlan(), toolGetChange, json.RawMessage(`{"address":"aws_s3_bucket.absent"}`))
	require.Error(t, err)
	// The error goes back as a tool result rather than failing the run, so the judge
	// can correct itself.
	require.Contains(t, body, "no change at")
}

func TestListChangesFilters(t *testing.T) {
	p := New(Options{})
	all, err := p.runTool(context.Background(), testPlan(), toolListChanges, json.RawMessage(`{}`))
	require.NoError(t, err)
	require.Contains(t, all, "aws_db_instance.orders")
	require.Contains(t, all, "aws_cloudwatch_metric_alarm.db")

	byAction, err := p.runTool(context.Background(), testPlan(), toolListChanges, json.RawMessage(`{"action":"destroy"}`))
	require.NoError(t, err)
	require.Contains(t, byAction, "aws_db_instance.orders")
	require.NotContains(t, byAction, "aws_cloudwatch_metric_alarm.db")

	none, err := p.runTool(context.Background(), testPlan(), toolListChanges, json.RawMessage(`{"type":"aws_sqs_queue"}`))
	require.NoError(t, err)
	require.Equal(t, `{"changes":[]}`, none)
}

func TestScoreProposition(t *testing.T) {
	var sent struct {
		State     jev.State                  `json:"state"`
		Questions map[string]json.RawMessage `json:"questions"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&sent))
		_, _ = w.Write([]byte(`{"answers":{"q":{"type":"noul","noul":0.81}},"usage":{"input_tokens":10}}`))
	}))
	defer srv.Close()

	p := New(Options{Scorer: jev.New(jev.Options{BaseURL: srv.URL, HTTP: srv.Client()})})
	body, err := p.runTool(context.Background(), testPlan(), toolScore, json.RawMessage(
		`{"address":"aws_db_instance.orders","instructions":"The change at `+"`focus`"+` has no snapshot.","when_true":"no snapshot attribute"}`))
	require.NoError(t, err)
	require.Contains(t, body, `"score":0.81`)
	// The proposition has to be aimed at the change the judge named, not at index 0 by
	// default, or the score answers about the wrong resource.
	require.Contains(t, string(sent.Questions["q"]), "`changes[0]`")
	require.Equal(t, "aws_db_instance.orders", sent.State.Changes[0].Address)
	require.NotNil(t, sent.State.Changes[0].After)
}

func TestScoreToolOfferedOnlyWithAScorer(t *testing.T) {
	names := func(p *Provider) []string {
		var out []string
		for _, t := range p.tools() {
			out = append(out, t.OfTool.Name)
		}
		return out
	}
	require.NotContains(t, names(New(Options{})), toolScore)
	require.Contains(t, names(New(Options{Scorer: jev.New(jev.Options{})})), toolScore)
	require.Contains(t, names(New(Options{})), toolReport)
}

func TestFirstMessageCarriesFocusAndCheckpoints(t *testing.T) {
	p := New(Options{Checkpoints: map[string][]model.Checkpoint{
		"aws_db_instance": {{ID: "rds-final-snapshot", Severity: model.SeverityCritical, Guidance: "Deleting without a final snapshot loses the data."}},
	}})
	got := p.firstMessage(
		requestFor(testPlan(), map[string][]string{"data-loss": {"aws_db_instance.orders"}}),
		model.Check{ID: "data-loss", Instructions: "It loses data.", Criteria: &model.Criteria{True: "a store is destroyed"}},
	)
	require.Contains(t, got, "It loses data. This holds when a store is destroyed.")
	require.Contains(t, got, "aws_db_instance.orders")
	require.Contains(t, got, "rds-final-snapshot")
	// The plan is listed as addresses only; the attributes are a tool call away.
	require.NotContains(t, got, "identifier")
}

func TestParseVerdictRejectsUnusable(t *testing.T) {
	ck := model.Check{ID: "polp"}
	for _, raw := range []string{
		`{"verdict":"maybe","reason":"r"}`,
		`{"verdict":"hit","reason":"  "}`,
		`not json`,
	} {
		if _, ok := parseVerdict(ck, json.RawMessage(raw)); ok {
			t.Errorf("%s should not be accepted as a verdict", raw)
		}
	}
	a, ok := parseVerdict(ck, json.RawMessage(`{"verdict":"miss","reason":"nothing widened"}`))
	require.True(t, ok)
	require.Equal(t, model.VerdictMiss, a.Kind)
	require.Equal(t, "nothing widened", a.Reason)
}

func requestFor(pl *plan.Plan, focus map[string][]string) llm.Request {
	return llm.Request{Plan: pl, Language: "en", Focus: focus, Checks: []model.Check{dataLoss()}}
}
