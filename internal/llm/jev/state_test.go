package jev

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nakamasato/tfreview/internal/plan"
)

func testPlan() *plan.Plan {
	return &plan.Plan{
		Target: "prd",
		Counts: plan.Counts{Change: 1, Replace: 1},
		Resources: []plan.Resource{
			{
				Address: "aws_s3_bucket.logs", Type: "aws_s3_bucket", Name: "logs",
				ProviderName: "registry.terraform.io/hashicorp/aws",
				Actions:      []string{"update"},
				ChangedKeys:  []string{"force_destroy"},
				After:        map[string]any{"bucket": "logs", "force_destroy": true, "tags": map[string]any{"env": "prd"}},
			},
			{
				Address: "aws_db_instance.orders", Type: "aws_db_instance", Name: "orders",
				ProviderName: "registry.terraform.io/hashicorp/aws",
				Actions:      []string{"delete", "create"},
				ActionReason: "replace_because_cannot_update",
				ReplacePaths: []string{"engine_version"},
				After:        map[string]any{"identifier": "orders", "policy": strings.Repeat("x", 50)},
			},
		},
	}
}

func TestBuildState(t *testing.T) {
	s := BuildState(testPlan(), 1, true, 20)

	if got := s.Changes[0]; got.After != nil {
		t.Errorf("change 0 is not the focus, after should be withheld: %v", got.After)
	}
	if got := s.Changes[0]; got.Provider != "aws" || got.Action != "change" {
		t.Errorf("change 0 = %+v", got)
	}
	// Tags stay visible without focus: which environment a change lands in decides
	// several checks, and a tag value is not a secret.
	if got := s.Changes[0].Tags["env"]; got != "prd" {
		t.Errorf("tags = %v", s.Changes[0].Tags)
	}
	f := s.Changes[1]
	if f.Action != "replace" || f.ActionReason != "replace_because_cannot_update" || f.ReplacePaths[0] != "engine_version" {
		t.Errorf("focus = %+v", f)
	}
	if f.After["identifier"] != "orders" {
		t.Errorf("focus after not disclosed: %v", f.After)
	}
	if got, ok := f.After["policy"].(string); !ok || !strings.HasSuffix(got, "(50 chars total)") {
		t.Errorf("long value not trimmed: %v", f.After["policy"])
	}
	if b, err := json.Marshal(s); err != nil {
		t.Fatal(err)
	} else if strings.Contains(string(b), `"before"`) {
		t.Error("state must never carry before")
	}
}

func TestBuildStateNoDisclose(t *testing.T) {
	for _, c := range BuildState(testPlan(), -1, false, 0).Changes {
		if c.After != nil {
			t.Errorf("change %d disclosed after: %v", c.I, c.After)
		}
	}
}

func TestHeaders(t *testing.T) {
	s := Headers(BuildState(testPlan(), 1, true, 0), 1)
	if s.Changes[0].ChangedKeys != nil || s.Changes[0].Tags != nil {
		t.Errorf("non-focus change kept detail: %+v", s.Changes[0])
	}
	if s.Changes[0].Address != "aws_s3_bucket.logs" {
		t.Errorf("non-focus change lost its identity: %+v", s.Changes[0])
	}
	if s.Changes[1].After == nil {
		t.Errorf("focus change lost its after: %+v", s.Changes[1])
	}
}

func TestFocus(t *testing.T) {
	qs := map[string]Question{"q": {
		Type:         "noul",
		Instructions: "A key in `focus.changed_keys` is a guard.",
		Criteria:     &Criteria{True: map[string]any{"note": "see `focus.after`"}},
	}}
	got := Focus(qs, 3)
	if s, _ := got["q"].Instructions.(string); !strings.Contains(s, "`changes[3].changed_keys`") {
		t.Errorf("instructions = %v", got["q"].Instructions)
	}
	m, _ := got["q"].Criteria.True.(map[string]any)
	if s, _ := m["note"].(string); !strings.Contains(s, "`changes[3].after`") {
		t.Errorf("nested criteria not rewritten: %v", got["q"].Criteria.True)
	}
	if s, _ := qs["q"].Instructions.(string); !strings.Contains(s, "`focus.") {
		t.Error("Focus mutated its input")
	}
}

func TestTrimKeepsKeys(t *testing.T) {
	in := map[string]any{
		"short":  "ok",
		"long":   strings.Repeat("y", 30),
		"nested": []any{map[string]any{"deep": strings.Repeat("z", 30)}},
		"empty":  "",
		"flag":   false,
	}
	out, _ := Trim(in, 10).(map[string]any)
	if len(out) != len(in) {
		t.Fatalf("keys dropped: %v", out)
	}
	if out["short"] != "ok" || out["flag"] != false {
		t.Errorf("short values changed: %v", out)
	}
	if got := out["long"].(string); got != "yyyyyyyyyy...(30 chars total)" {
		t.Errorf("long = %q", got)
	}
	deep := out["nested"].([]any)[0].(map[string]any)["deep"].(string)
	if !strings.HasSuffix(deep, "(30 chars total)") {
		t.Errorf("nested value not trimmed: %q", deep)
	}
}
