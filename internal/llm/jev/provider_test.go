package jev

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nakamasato/tfreview/internal/llm"
	"github.com/nakamasato/tfreview/internal/model"
	"github.com/nakamasato/tfreview/internal/plan"
)

func checks() []model.Check {
	return []model.Check{
		{ID: "polp", Instructions: "The change at `focus` widens access."},
		{ID: "stateful-delete", Match: model.Match{Types: []string{"aws_db_instance"}}},
		{ID: "cost", Instructions: "The change at `focus` costs more.",
			Match: model.Match{Actions: []string{"create"}}},
	}
}

func threeResources() *plan.Plan {
	return &plan.Plan{Target: "prd", Counts: plan.Counts{Add: 1, Change: 1, Destroy: 1}, Resources: []plan.Resource{
		{Address: "aws_sqs_queue.new", Type: "aws_sqs_queue", Actions: []string{"create"}, After: map[string]any{"name": "q"}},
		{Address: "aws_s3_bucket.logs", Type: "aws_s3_bucket", Actions: []string{"update"}, After: map[string]any{"acl": "public-read"}},
		{Address: "aws_db_instance.orders", Type: "aws_db_instance", Actions: []string{"delete"}},
	}}
}

// scoreServer answers with a fixed score per address, so a test can say which change
// carries the risk and then assert on what the provider reduced the plan to.
func scoreServer(t *testing.T, byAddress map[string]float64) (*httptest.Server, *int32) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		b, _ := io.ReadAll(r.Body)
		var req struct {
			State     State                      `json:"state"`
			Questions map[string]json.RawMessage `json:"questions"`
		}
		if err := json.Unmarshal(b, &req); err != nil {
			t.Error(err)
		}
		// The focused change is the only one carrying `after`.
		addr := ""
		for _, c := range req.State.Changes {
			if c.After != nil {
				addr = c.Address
			}
		}
		answers := map[string]any{}
		for id := range req.Questions {
			answers[id] = map[string]any{"type": "noul", "noul": byAddress[addr]}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"answers": answers, "usage": map[string]int{"input_tokens": 100}})
	}))
	return srv, &calls
}

func newTestProvider(url string, client *http.Client) *Provider {
	return NewProvider(ProviderOptions{
		Options:       Options{BaseURL: url, HTTP: client},
		HitThreshold:  0.70,
		MissThreshold: 0.30,
		Concurrency:   2,
	})
}

func TestProviderReducesToHighestScore(t *testing.T) {
	srv, calls := scoreServer(t, map[string]float64{
		"aws_sqs_queue.new":      0.05,
		"aws_s3_bucket.logs":     0.91,
		"aws_db_instance.orders": 0.10,
	})
	defer srv.Close()

	got, usage, err := newTestProvider(srv.URL, srv.Client()).
		Judge(context.Background(), llm.Request{Plan: threeResources(), Checks: checks()})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]llm.Answer{}
	for _, a := range got {
		byID[a.CheckID] = a
	}
	// stateful-delete is rule-only, so it is never sent.
	if _, ok := byID["stateful-delete"]; ok {
		t.Error("a rule-only check should not be scored")
	}
	if a := byID["polp"]; a.Kind != model.VerdictHit || a.Score != 0.91 {
		t.Errorf("polp = %+v", a)
	}
	if a := byID["polp"]; !strings.Contains(a.Reason, "aws_s3_bucket.logs (0.91)") {
		t.Errorf("the reason should name the change that scored: %q", a.Reason)
	}
	// cost has match.actions [create], so only the one created change is asked, and it
	// scored 0.05 — a miss even though another change scored 0.91 for polp.
	if a := byID["cost"]; a.Kind != model.VerdictMiss || a.Score != 0.05 {
		t.Errorf("cost = %+v", a)
	}
	// One call per change, not one per change per check.
	if *calls != 3 {
		t.Errorf("calls = %d, want 3", *calls)
	}
	if usage.Calls != 3 || usage.InputTokens != 300 {
		t.Errorf("usage = %+v", usage)
	}
}

func TestProviderUndecidedBandIsUnverifiable(t *testing.T) {
	srv, _ := scoreServer(t, map[string]float64{"aws_s3_bucket.logs": 0.55})
	defer srv.Close()

	got, _, err := newTestProvider(srv.URL, srv.Client()).
		Judge(context.Background(), llm.Request{Plan: threeResources(), Checks: checks()})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range got {
		if a.CheckID != "polp" {
			continue
		}
		if a.Kind != model.VerdictUnverifiable {
			t.Errorf("a score between the thresholds should not settle the check: %+v", a)
		}
		if !strings.Contains(a.Reason, "closer look") {
			t.Errorf("reason = %q", a.Reason)
		}
	}
}

func TestProviderFallsBackToHeadersWhenTooLarge(t *testing.T) {
	var bodies []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, len(b))
		_, _ = w.Write([]byte(`{"answers":{"polp":{"type":"noul","noul":0.9}},"usage":{}}`))
	}))
	defer srv.Close()

	// Only the non-focus changes are cut back, so the detail that makes the request
	// too large has to sit on them: changed_keys is what scales with plan size.
	pl := threeResources()
	for i := range pl.Resources {
		for k := 0; k < 40; k++ {
			pl.Resources[i].ChangedKeys = append(pl.Resources[i].ChangedKeys, strings.Repeat("k", 20))
		}
	}
	ck := checks()[:1]
	full, _ := json.Marshal(request{State: BuildState(pl, 0, true, 0), Questions: Focus(Questions(ck), 0)})
	headers, _ := json.Marshal(request{State: Headers(BuildState(pl, 0, true, 0), 0), Questions: Focus(Questions(ck), 0)})
	if len(headers) >= len(full) {
		t.Fatalf("the fallback must be smaller: headers=%d full=%d", len(headers), len(full))
	}

	p := NewProvider(ProviderOptions{
		Options:       Options{BaseURL: srv.URL, HTTP: srv.Client(), MaxRequestChars: len(full) - 1},
		HitThreshold:  0.70,
		MissThreshold: 0.30,
		Concurrency:   1,
	})
	got, _, err := p.Judge(context.Background(), llm.Request{Plan: pl, Checks: ck})
	if err != nil {
		t.Fatal(err)
	}
	if len(bodies) != len(pl.Resources) {
		t.Fatalf("every change should have been sent once on retry: %v", bodies)
	}
	for _, n := range bodies {
		if n >= len(full) {
			t.Errorf("a body of %d was not cut back below %d", n, len(full))
		}
	}
	for _, a := range got {
		if a.CheckID == "polp" && a.Kind != model.VerdictHit {
			t.Errorf("the header-only retry should still produce a verdict: %+v", a)
		}
	}
}

func TestProviderNoChanges(t *testing.T) {
	got, usage, err := newTestProvider("http://127.0.0.1:0", nil).
		Judge(context.Background(), llm.Request{Plan: &plan.Plan{Target: "prd"}, Checks: checks()})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 || usage.Calls != 0 {
		t.Errorf("an empty plan should cost nothing: got=%v usage=%+v", got, usage)
	}
}
