package deepdive

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/nakamasato/tfreview/internal/llm/jev"
	"github.com/nakamasato/tfreview/internal/plan"
)

const (
	toolGetChange   = "get_change"
	toolListChanges = "list_changes"
	toolScore       = "score_proposition"
	toolReport      = "report_verdict"
)

func (p *Provider) tools() []sdk.ToolUnionParam {
	defs := []sdk.ToolParam{
		{
			Name:        toolGetChange,
			Description: sdk.String("Everything the plan holds about one change: its resulting attributes, which attributes changed, and the changes it references or is referenced by."),
			InputSchema: sdk.ToolInputSchemaParam{
				Properties: map[string]any{
					"address": map[string]any{"type": "string", "description": "The change's address, exactly as listed."},
				},
				Required: []string{"address"},
			},
		},
		{
			Name:        toolListChanges,
			Description: sdk.String("Addresses of the changes in this plan, optionally narrowed by resource type or action (add, change, destroy, replace)."),
			InputSchema: sdk.ToolInputSchemaParam{
				Properties: map[string]any{
					"type":   map[string]any{"type": "string"},
					"action": map[string]any{"type": "string", "enum": []string{"add", "change", "destroy", "replace"}},
				},
			},
		},
		{
			Name:        toolReport,
			Description: sdk.String("Report the verdict for the proposition. Call this exactly once, when you are done looking."),
			InputSchema: sdk.ToolInputSchemaParam{
				Properties: map[string]any{
					"verdict": map[string]any{"type": "string", "enum": []string{"hit", "miss", "unverifiable"}},
					"reason":  map[string]any{"type": "string"},
				},
				Required: []string{"verdict", "reason"},
			},
		},
	}
	if p.opts.Scorer != nil {
		// Offered only when a scoring judge is configured: a proposition of the judge's
		// own gets a calibrated probability instead of its own impression.
		defs = append(defs, sdk.ToolParam{
			Name:        toolScore,
			Description: sdk.String("Score a proposition of your own about one change, from 0 to 1. State it as something that is either the case or not, and say what would put it on each side."),
			InputSchema: sdk.ToolInputSchemaParam{
				Properties: map[string]any{
					"address":      map[string]any{"type": "string"},
					"instructions": map[string]any{"type": "string", "description": "The proposition, as a statement rather than a question."},
					"when_true":    map[string]any{"type": "string"},
					"when_false":   map[string]any{"type": "string"},
				},
				Required: []string{"address", "instructions"},
			},
		})
	}
	out := make([]sdk.ToolUnionParam, 0, len(defs))
	for i := range defs {
		out = append(out, sdk.ToolUnionParam{OfTool: &defs[i]})
	}
	return out
}

func (p *Provider) runTool(ctx context.Context, pl *plan.Plan, name string, input json.RawMessage) (string, error) {
	switch name {
	case toolGetChange:
		return p.getChange(pl, input)
	case toolListChanges:
		return listChanges(pl, input)
	case toolScore:
		return p.score(ctx, pl, input)
	}
	return toolError(fmt.Errorf("unknown tool %q", name))
}

func toolError(err error) (string, error) {
	b, _ := json.Marshal(map[string]string{"error": err.Error()})
	return string(b), err
}

func (p *Provider) getChange(pl *plan.Plan, input json.RawMessage) (string, error) {
	var in struct {
		Address string `json:"address"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return toolError(err)
	}
	i := indexOf(pl, in.Address)
	if i < 0 {
		return toolError(fmt.Errorf("no change at %q in this plan", in.Address))
	}
	// Reuse the state builder so a change reads the same here as it did when it was
	// scored, including the trimming of long values.
	state := jev.BuildState(pl, i, true, p.opts.MaxValueChars)
	b, err := json.MarshalIndent(state.Changes[i], "", "  ")
	if err != nil {
		return toolError(err)
	}
	return string(b), nil
}

func listChanges(pl *plan.Plan, input json.RawMessage) (string, error) {
	var in struct {
		Type   string `json:"type"`
		Action string `json:"action"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return toolError(err)
	}
	var out []string
	for _, c := range jev.BuildState(pl, -1, false, 0).Changes {
		if in.Type != "" && c.Type != in.Type {
			continue
		}
		if in.Action != "" && c.Action != in.Action {
			continue
		}
		out = append(out, fmt.Sprintf("%s (%s %s)", c.Address, c.Action, c.Type))
	}
	if len(out) == 0 {
		return `{"changes":[]}`, nil
	}
	b, _ := json.Marshal(map[string][]string{"changes": out})
	return string(b), nil
}

func (p *Provider) score(ctx context.Context, pl *plan.Plan, input json.RawMessage) (string, error) {
	var in struct {
		Address      string `json:"address"`
		Instructions string `json:"instructions"`
		WhenTrue     string `json:"when_true"`
		WhenFalse    string `json:"when_false"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return toolError(err)
	}
	if strings.TrimSpace(in.Instructions) == "" {
		return toolError(fmt.Errorf("instructions must not be empty"))
	}
	i := indexOf(pl, in.Address)
	if i < 0 {
		return toolError(fmt.Errorf("no change at %q in this plan", in.Address))
	}
	q := jev.Question{Type: "noul", Instructions: in.Instructions}
	if in.WhenTrue != "" || in.WhenFalse != "" {
		q.Criteria = &jev.Criteria{True: in.WhenTrue, False: in.WhenFalse}
	}
	resp, err := p.opts.Scorer.Ask(ctx,
		jev.BuildState(pl, i, true, p.opts.MaxValueChars),
		jev.Focus(map[string]jev.Question{"q": q}, i))
	if err != nil {
		return toolError(err)
	}
	b, _ := json.Marshal(map[string]any{
		"address": in.Address,
		"score":   resp.Answers["q"].Noul,
		"note":    "A probability, not a verdict. Near 0.5 means the proposition is as supported as its opposite.",
	})
	return string(b), nil
}

func indexOf(pl *plan.Plan, address string) int {
	for i, r := range pl.Resources {
		if r.Address == address {
			return i
		}
	}
	return -1
}
