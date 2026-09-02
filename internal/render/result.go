// Package render は judge の出力を result.json と PR コメントに変換する。
package render

import (
	"encoding/json"
	"os"
	"sort"

	"github.com/nakamasato/tfreview/internal/config"
	"github.com/nakamasato/tfreview/internal/judge"
	"github.com/nakamasato/tfreview/internal/llm"
	"github.com/nakamasato/tfreview/internal/model"
	"github.com/nakamasato/tfreview/internal/plan"
)

type CheckResult struct {
	ID      string            `json:"id"`
	Level   model.Level       `json:"level"`
	Verdict model.VerdictKind `json:"verdict"`
	Reason  string            `json:"reason"`
	Source  model.Source      `json:"source"`
}

type CategoryResult struct {
	ID     string        `json:"id"`
	Title  string        `json:"title"`
	Score  model.Level   `json:"score"`
	Hits   int           `json:"hits"`
	Total  int           `json:"total"`
	Checks []CheckResult `json:"checks"`
}

type TargetResult struct {
	Target string      `json:"target"`
	Counts plan.Counts `json:"counts"`
	Reused bool        `json:"reused"`
}

type Result struct {
	Score        model.Level      `json:"score"`
	MachineScore model.Level      `json:"machine_score"`
	Incomplete   bool             `json:"incomplete"`
	Label        string           `json:"label"`
	HeadSHA      string           `json:"head_sha"`
	JudgedAt     string           `json:"judged_at"`
	Repo         string           `json:"repo"`
	ConfigPath   string           `json:"config_path"`
	Language     string           `json:"language"`
	Model        string           `json:"model"`
	NoPlans      bool             `json:"no_plans"`
	NoChanges    bool             `json:"no_changes"`
	Categories   []CategoryResult `json:"categories"`
	Targets      []TargetResult   `json:"targets"`
	Unevaluated  []string         `json:"unevaluated"`
	Usage        llm.Usage        `json:"usage"`
	CostUSD      float64          `json:"cost_usd"`
}

type Meta struct {
	HeadSHA    string
	JudgedAt   string
	Repo       string
	ConfigPath string
	Model      string
	Pricing    llm.Pricing
}

func Build(cfg *config.Config, out *judge.Output, meta Meta) *Result {
	r := &Result{
		HeadSHA: meta.HeadSHA, JudgedAt: meta.JudgedAt, Repo: meta.Repo, ConfigPath: meta.ConfigPath,
		Language: cfg.Language, Model: meta.Model, NoPlans: out.NoPlans, NoChanges: out.NoChanges,
		Usage: out.Usage, Unevaluated: []string{}, Targets: []TargetResult{}, Categories: []CategoryResult{},
	}
	r.CostUSD = out.Usage.Cost(meta.Pricing)
	for _, t := range out.Targets {
		r.Targets = append(r.Targets, TargetResult{Target: t.Target, Counts: t.Counts, Reused: t.Reused})
	}
	for id := range out.Unevaluated {
		r.Unevaluated = append(r.Unevaluated, id)
	}
	sort.Strings(r.Unevaluated)

	for _, cat := range cfg.Categories {
		cr := CategoryResult{ID: cat.ID, Title: cat.Title, Score: judge.CategoryScore(cat, out.Verdicts), Total: len(cat.Checks), Checks: []CheckResult{}}
		for _, ck := range cat.Checks {
			v, ok := out.Verdicts[ck.ID]
			if !ok {
				v = model.Verdict{CheckID: ck.ID, Kind: model.VerdictSkipped, Reason: "not evaluated", Source: model.SourceLLM}
			}
			if v.Kind == model.VerdictHit || v.Kind == model.VerdictUnverifiable {
				cr.Hits++
			}
			cr.Checks = append(cr.Checks, CheckResult{ID: ck.ID, Level: ck.Level, Verdict: v.Kind, Reason: v.Reason, Source: v.Source})
		}
		r.Categories = append(r.Categories, cr)
	}

	r.Score = judge.Score(cfg, out.Verdicts)
	r.MachineScore = judge.MachineScore(cfg, out.Verdicts)
	r.Incomplete = !out.NoPlans && !out.NoChanges && judge.IsIncomplete(out.Verdicts, out.Unevaluated)
	if r.Incomplete {
		r.Label = "tfreview:unknown"
	} else {
		r.Label = "tfreview:" + string(r.Score)
	}
	return r
}

func (r *Result) Save(path string) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func LoadResult(path string) (*Result, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r Result
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

var labelColors = map[string]string{
	"tfreview:none":     "0E8A16",
	"tfreview:medium":   "FBCA04",
	"tfreview:high":     "D93F0B",
	"tfreview:critical": "B60205",
	"tfreview:unknown":  "0075CA",
}

func LabelColor(label string) string {
	if c, ok := labelColors[label]; ok {
		return c
	}
	return labelColors["tfreview:unknown"]
}
