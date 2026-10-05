// Package state caches LLM verdicts keyed by a plan+config digest.
// The goal is judgment stability more than token cost — never re-judge the same plan.
package state

import (
	"encoding/json"
	"os"

	"github.com/nakamasato/tfreview/internal/model"
)

type TargetState struct {
	PlanDigest string          `json:"plan_digest"`
	Verdicts   []model.Verdict `json:"verdicts"`
	// Primary and Deep are each phase's own results, which Verdicts has merged away.
	// They let a reused target render the same phase sections as the run that judged it.
	Primary []model.Verdict `json:"primary,omitempty"`
	Deep    []model.Verdict `json:"deep,omitempty"`
}

type State struct {
	HeadSHA      string                 `json:"head_sha"`
	ConfigDigest string                 `json:"config_digest"`
	Targets      map[string]TargetState `json:"targets"`
}

func New(headSHA, configDigest string) *State {
	return &State{HeadSHA: headSHA, ConfigDigest: configDigest, Targets: map[string]TargetState{}}
}

// Load swallows any failure and returns an empty state. State is an optimization,
// not a source of correctness, so if it's corrupt, just re-judge every target.
func Load(path string) *State {
	empty := New("", "")
	if path == "" {
		return empty
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return empty
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil || s.Targets == nil {
		return empty
	}
	return &s
}

func (s *State) Save(path string) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func (s *State) Reusable(target, planDigest, configDigest string) ([]model.Verdict, bool) {
	if s.ConfigDigest != configDigest {
		return nil, false
	}
	ts, ok := s.Targets[target]
	if !ok || ts.PlanDigest != planDigest || hasSkipped(ts.Verdicts) {
		return nil, false
	}
	return ts.Verdicts, true
}

// Writing an unevaluated verdict would let a brief API outage get baked in for the PR's whole lifetime.
func (s *State) Put(target, planDigest string, verdicts []model.Verdict) {
	if hasSkipped(verdicts) {
		return
	}
	s.Targets[target] = TargetState{PlanDigest: planDigest, Verdicts: verdicts}
}

// PutPhases attaches per-phase verdicts to a target Put has stored. It is a no-op for a
// target Put rejected, so phase data never outlives the verdicts it belongs to.
func (s *State) PutPhases(target string, primary, deep []model.Verdict) {
	if ts, ok := s.Targets[target]; ok {
		ts.Primary, ts.Deep = primary, deep
		s.Targets[target] = ts
	}
}

func hasSkipped(vs []model.Verdict) bool {
	for _, v := range vs {
		if v.Kind == model.VerdictSkipped {
			return true
		}
	}
	return false
}
