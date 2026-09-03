// Package state は LLM 判定を plan+config の digest でキャッシュする。
// 目的はトークン代よりも判定の安定性（同じ plan に対して判定を作り直さない）。
package state

import (
	"encoding/json"
	"os"

	"github.com/nakamasato/tfreview/internal/model"
)

type TargetState struct {
	PlanDigest string          `json:"plan_digest"`
	Verdicts   []model.Verdict `json:"verdicts"`
}

type State struct {
	HeadSHA      string                 `json:"head_sha"`
	ConfigDigest string                 `json:"config_digest"`
	Targets      map[string]TargetState `json:"targets"`
}

func New(headSHA, configDigest string) *State {
	return &State{HeadSHA: headSHA, ConfigDigest: configDigest, Targets: map[string]TargetState{}}
}

// Load は失敗を握りつぶして空を返す。state は最適化であって正しさの根拠ではないので、
// 壊れていたら全 target を判定し直せばよい。
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

// 評価できなかった判定を書くと、数十秒の API 障害が PR の寿命のあいだ固定される。
func (s *State) Put(target, planDigest string, verdicts []model.Verdict) {
	if hasSkipped(verdicts) {
		return
	}
	s.Targets[target] = TargetState{PlanDigest: planDigest, Verdicts: verdicts}
}

func hasSkipped(vs []model.Verdict) bool {
	for _, v := range vs {
		if v.Kind == model.VerdictSkipped {
			return true
		}
	}
	return false
}
