package checkpoint

import (
	"testing"

	"github.com/nakamasato/tfreview/internal/model"
	"github.com/nakamasato/tfreview/internal/plan"
)

func cps() map[string][]model.Checkpoint {
	return map[string][]model.Checkpoint{
		"aws_db_instance": {
			{ID: "rds-guard", Aspect: "data-loss", Severity: model.SeverityCritical, Guidance: "g"},
			{ID: "rds-lifecycle", Aspect: "data-loss", Severity: model.SeverityHigh, Guidance: "g", Requires: []string{model.RequiresDiff}},
		},
		"aws_s3_bucket": {
			{ID: "s3-public", Aspect: "exposure", Severity: model.SeverityHigh, Guidance: "g"},
		},
	}
}

func testPlan() *plan.Plan {
	return &plan.Plan{Target: "prd", Resources: []plan.Resource{
		{Address: "aws_db_instance.main", Type: "aws_db_instance", Actions: []string{"update"}},
		{Address: "aws_db_instance.replica", Type: "aws_db_instance", Actions: []string{"update"}},
		{Address: "aws_iam_role.app", Type: "aws_iam_role", Actions: []string{"create"}},
	}}
}

func TestSelectKeepsOnlyTypesInPlan(t *testing.T) {
	got := Select(cps(), testPlan(), Inputs{Diff: true})
	if len(got) != 1 {
		t.Fatalf("Select returned %d groups, want 1", len(got))
	}
	g := got[0]
	if g.ResourceType != "aws_db_instance" {
		t.Errorf("ResourceType = %q, want aws_db_instance", g.ResourceType)
	}
	if len(g.Addresses) != 2 {
		t.Errorf("Addresses = %v, want 2 entries", g.Addresses)
	}
	if len(g.Checkpoints) != 2 {
		t.Errorf("Checkpoints = %d, want 2", len(g.Checkpoints))
	}
}

func TestSelectDropsUnmetRequirements(t *testing.T) {
	got := Select(cps(), testPlan(), Inputs{})
	if len(got) != 1 || len(got[0].Checkpoints) != 1 {
		t.Fatalf("Select = %+v, want 1 group with 1 checkpoint", got)
	}
	if got[0].Checkpoints[0].ID != "rds-guard" {
		t.Errorf("kept %q, want rds-guard", got[0].Checkpoints[0].ID)
	}
	held := Held(cps(), testPlan(), Inputs{})
	if len(held) != 1 || held[0].ID != "rds-lifecycle" {
		t.Errorf("Held = %+v, want rds-lifecycle", held)
	}
}

func TestSelectEmpty(t *testing.T) {
	if got := Select(nil, testPlan(), Inputs{}); len(got) != 0 {
		t.Errorf("Select with no checkpoints = %+v, want empty", got)
	}
	if got := Select(cps(), &plan.Plan{Target: "prd"}, Inputs{}); len(got) != 0 {
		t.Errorf("Select with an empty plan = %+v, want empty", got)
	}
}

func TestSelectIsDeterministic(t *testing.T) {
	p := &plan.Plan{Target: "prd", Resources: []plan.Resource{
		{Address: "aws_s3_bucket.b", Type: "aws_s3_bucket"},
		{Address: "aws_db_instance.main", Type: "aws_db_instance"},
	}}
	first := Select(cps(), p, Inputs{Diff: true})
	for i := 0; i < 10; i++ {
		got := Select(cps(), p, Inputs{Diff: true})
		for j := range got {
			if got[j].ResourceType != first[j].ResourceType {
				t.Fatalf("group order changed: %q vs %q", got[j].ResourceType, first[j].ResourceType)
			}
		}
	}
}
