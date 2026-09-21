package deepdive

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nakamasato/tfreview/internal/model"
	"github.com/stretchr/testify/require"
)

// turn is one scripted reply from the Messages API: either a tool call or a verdict.
type turn struct {
	tool  string
	input string
}

// scriptedAPI replies with the given turns in order and records what it was sent, so the
// tool loop can be driven end to end without an API key.
func scriptedAPI(t *testing.T, turns []turn) (*httptest.Server, *[][]any) {
	var seen [][]any
	step := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content []any `json:"content"`
			} `json:"messages"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		w.Header().Set("Content-Type", "application/json")
		var last []any
		if n := len(body.Messages); n > 0 {
			last = body.Messages[n-1].Content
		}
		seen = append(seen, last)

		if step >= len(turns) {
			// Text only: no tool call and no verdict, which the loop has to treat as the
			// end of a conversation that concluded nothing.
			_, _ = fmt.Fprint(w, `{"id":"m","type":"message","role":"assistant","model":"m","stop_reason":"end_turn","content":[{"type":"text","text":"done"}],"usage":{"input_tokens":5,"output_tokens":1}}`)
			return
		}
		tn := turns[step]
		step++
		_, _ = fmt.Fprintf(w, `{"id":"m","type":"message","role":"assistant","model":"m","stop_reason":"tool_use","usage":{"input_tokens":5,"output_tokens":1},
			"content":[{"type":"tool_use","id":"tu%d","name":%q,"input":%s}]}`, step, tn.tool, tn.input)
	}))
	return srv, &seen
}

func provider(t *testing.T, turns []turn, maxSteps int) (*Provider, *[][]any) {
	srv, seen := scriptedAPI(t, turns)
	t.Cleanup(srv.Close)
	return New(Options{Model: "m", APIKey: "k", BaseURL: srv.URL, MaxSteps: maxSteps}), seen
}

func dataLoss() model.Check {
	return model.Check{ID: "data-loss", Severity: model.SeverityCritical, Instructions: "It loses data."}
}

func TestLoopLooksThenReports(t *testing.T) {
	p, seen := provider(t, []turn{
		{toolGetChange, `{"address":"aws_db_instance.orders"}`},
		{toolReport, `{"verdict":"hit","reason":"aws_cloudwatch_metric_alarm.db points at it"}`},
	}, 8)

	got, usage, err := p.Judge(context.Background(), requestFor(testPlan(), map[string][]string{"data-loss": {"aws_db_instance.orders"}}))
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, model.VerdictHit, got[0].Kind)
	require.Equal(t, "aws_cloudwatch_metric_alarm.db points at it", got[0].Reason)
	require.Equal(t, 2, usage.Calls)
	require.Equal(t, int64(10), usage.InputTokens)

	// The second request carries the tool's output back, so the loop really fed the
	// plan to the judge rather than looping on its own words.
	require.Len(t, *seen, 2)
	second, _ := json.Marshal((*seen)[1])
	require.Contains(t, string(second), "aws_cloudwatch_metric_alarm.db")
}

func TestLoopRejectsABadVerdictAndCarriesOn(t *testing.T) {
	p, _ := provider(t, []turn{
		{toolReport, `{"verdict":"probably","reason":"r"}`},
		{toolReport, `{"verdict":"miss","reason":"nothing is destroyed"}`},
	}, 8)

	got, _, err := p.Judge(context.Background(), requestFor(testPlan(), nil))
	require.NoError(t, err)
	require.Equal(t, model.VerdictMiss, got[0].Kind)
}

func TestLoopRunsOutOfSteps(t *testing.T) {
	p, _ := provider(t, []turn{
		{toolListChanges, `{}`},
		{toolListChanges, `{}`},
		{toolListChanges, `{}`},
	}, 2)

	got, usage, err := p.Judge(context.Background(), requestFor(testPlan(), nil))
	require.NoError(t, err)
	// Out of steps is not a miss: the check stays undecided and says so.
	require.Equal(t, model.VerdictUnverifiable, got[0].Kind)
	require.Contains(t, got[0].Reason, "ran out of steps")
	require.Equal(t, 2, usage.Calls)
}

func TestLoopEndsWithoutAVerdict(t *testing.T) {
	p, _ := provider(t, nil, 8)
	got, _, err := p.Judge(context.Background(), requestFor(testPlan(), nil))
	require.NoError(t, err)
	require.Equal(t, model.VerdictUnverifiable, got[0].Kind)
	require.Contains(t, got[0].Reason, "without a verdict")
}

func TestLoopReportsAnAPIFailureAsSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"nope"}}`))
	}))
	defer srv.Close()

	p := New(Options{Model: "m", APIKey: "k", BaseURL: srv.URL})
	got, _, err := p.Judge(context.Background(), requestFor(testPlan(), nil))
	require.NoError(t, err)
	// Skipped, not unverifiable: judge.deepen then keeps the first pass's verdict.
	require.Equal(t, model.VerdictSkipped, got[0].Kind)
	require.Contains(t, got[0].Reason, "closer look failed")
}

func TestLoopJudgesEachCheckSeparately(t *testing.T) {
	p, seen := provider(t, []turn{
		{toolReport, `{"verdict":"hit","reason":"a"}`},
		{toolReport, `{"verdict":"miss","reason":"b"}`},
	}, 8)
	req := requestFor(testPlan(), nil)
	req.Checks = []model.Check{dataLoss(), {ID: "polp", Instructions: "It widens access."}}

	got, _, err := p.Judge(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, model.VerdictHit, got[0].Kind)
	require.Equal(t, model.VerdictMiss, got[1].Kind)
	// Each check starts its own conversation, so neither carries the other's turns.
	require.Len(t, *seen, 2)
}
