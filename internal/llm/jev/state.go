package jev

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nakamasato/tfreview/internal/plan"
)

// FocusPlaceholder is what a configured question writes where an index would go, so
// one question text can be aimed at any change: a question mentioning
// `focus.changed_keys` becomes `changes[3].changed_keys` for the fourth change.
const FocusPlaceholder = "focus"

// Change is one change as Jev sees it. Everything but After is metadata that reveals
// which keys moved without revealing any value, which is what lets most questions be
// decided before any attribute is disclosed.
type Change struct {
	I            int            `json:"i"`
	Address      string         `json:"address"`
	Type         string         `json:"type"`
	Provider     string         `json:"provider"`
	Action       string         `json:"action"`
	ActionReason string         `json:"action_reason,omitempty"`
	ReplacePaths []string       `json:"replace_paths,omitempty"`
	ChangedKeys  []string       `json:"changed_keys,omitempty"`
	UnknownKeys  []string       `json:"unknown_keys,omitempty"`
	Refs         []string       `json:"refs,omitempty"`
	ReferredBy   []string       `json:"referred_by,omitempty"`
	Tags         map[string]any `json:"tags,omitempty"`
	Labels       map[string]any `json:"labels,omitempty"`
	After        map[string]any `json:"after,omitempty"`
}

// State is the JSON object handed to Jev. `before` is deliberately absent: it never
// leaves the runner, so the direction a value moved is only recoverable for booleans
// (present in changed_keys and now false means it was true).
type State struct {
	Target  string      `json:"target"`
	Counts  plan.Counts `json:"counts"`
	Changes []Change    `json:"changes"`
}

// BuildState lists every change as metadata, and discloses the full `after` of the
// change at focus when disclose is set. The whole list is always present so a
// question can weigh one change against the rest of the plan; pass focus < 0 to
// disclose nothing.
//
// maxValueChars shortens long individual values inside the disclosed `after`. 0
// leaves them alone.
func BuildState(p *plan.Plan, focus int, disclose bool, maxValueChars int) State {
	s := State{Target: p.Target, Counts: p.Counts, Changes: make([]Change, 0, len(p.Resources))}
	for i, r := range p.Resources {
		c := Change{
			I:            i,
			Address:      r.Address,
			Type:         r.Type,
			Provider:     providerShortName(r.ProviderName),
			Action:       action(r.Actions),
			ActionReason: r.ActionReason,
			ReplacePaths: r.ReplacePaths,
			ChangedKeys:  r.ChangedKeys,
			UnknownKeys:  r.UnknownKeys,
			Refs:         r.Refs,
			ReferredBy:   r.ReferredBy,
			Tags:         asMap(r.After["tags"]),
			Labels:       asMap(r.After["labels"]),
		}
		if disclose && i == focus {
			c.After = trimMap(r.After, maxValueChars)
		}
		s.Changes = append(s.Changes, c)
	}
	return s
}

// Headers drops everything but the identity of each change except the one at focus.
// A plan with hundreds of changes spends its whole budget on a list nobody asked
// about, and Jev reads unrelated state as a distractor.
func Headers(s State, focus int) State {
	out := s
	out.Changes = make([]Change, 0, len(s.Changes))
	for _, c := range s.Changes {
		if c.I != focus {
			c = Change{I: c.I, Address: c.Address, Type: c.Type, Provider: c.Provider, Action: c.Action}
		}
		out.Changes = append(out.Changes, c)
	}
	return out
}

// Focus points every question in qs at the change at index i by rewriting
// FocusPlaceholder in the backtick paths the questions use to reference state.
func Focus(qs map[string]Question, i int) map[string]Question {
	// The rewrite runs over the marshalled JSON because instructions and criteria may
	// each be a nested structure, and the placeholder can sit at any depth in either.
	b, err := json.Marshal(qs)
	if err != nil {
		return qs
	}
	b = []byte(strings.ReplaceAll(string(b), "`"+FocusPlaceholder, fmt.Sprintf("`changes[%d]", i)))
	var out map[string]Question
	if err := json.Unmarshal(b, &out); err != nil {
		return qs
	}
	return out
}

// Trim shortens long string values. Keys are always kept: whether a key is present is
// itself evidence, so dropping one can silently flip a verdict, while a truncated
// value at least shows that it was cut.
func Trim(v any, maxChars int) any {
	if maxChars <= 0 {
		return v
	}
	switch t := v.(type) {
	case string:
		if len(t) <= maxChars {
			return t
		}
		return fmt.Sprintf("%s...(%d chars total)", t[:maxChars], len(t))
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, item := range t {
			out[k] = Trim(item, maxChars)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = Trim(item, maxChars)
		}
		return out
	}
	return v
}

func trimMap(m map[string]any, maxChars int) map[string]any {
	if m == nil {
		return nil
	}
	out, _ := Trim(m, maxChars).(map[string]any)
	return out
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	if len(m) == 0 {
		return nil
	}
	return m
}

// action names the change the way the counts do, falling back to the raw actions so a
// no-op adoption (`importing` with no change) still reads sensibly.
func action(actions []string) string {
	if k := plan.Kind(actions); k != "" {
		return k
	}
	return strings.Join(actions, "+")
}

func providerShortName(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}
