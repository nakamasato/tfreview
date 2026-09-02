// Package llm は判定に使う LLM の抽象。実装は anthropic と mock。
package llm

import (
	"context"

	"github.com/nakamasato/tfreview/internal/model"
	"github.com/nakamasato/tfreview/internal/plan"
)

type Request struct {
	Plan     *plan.Plan
	Checks   []model.Check
	Language string
}

type Answer struct {
	CheckID string
	Kind    model.VerdictKind
	Reason  string
}

type Usage struct {
	Calls            int   `json:"calls"`
	InputTokens      int64 `json:"input_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
}

func (u *Usage) Add(o Usage) {
	u.Calls += o.Calls
	u.InputTokens += o.InputTokens
	u.CacheWriteTokens += o.CacheWriteTokens
	u.CacheReadTokens += o.CacheReadTokens
	u.OutputTokens += o.OutputTokens
}

// Pricing は USD / Mtok。コメントのフッターに概算を出すためだけに使い、判定には効かない。
type Pricing struct {
	Input      float64
	CacheWrite float64
	CacheRead  float64
	Output     float64
}

var DefaultPricing = Pricing{Input: 5.00, CacheWrite: 6.25, CacheRead: 0.50, Output: 25.00}

func PricingFromMap(m map[string]float64) Pricing {
	p := DefaultPricing
	if v, ok := m["input"]; ok {
		p.Input = v
	}
	if v, ok := m["cache_write"]; ok {
		p.CacheWrite = v
	}
	if v, ok := m["cache_read"]; ok {
		p.CacheRead = v
	}
	if v, ok := m["output"]; ok {
		p.Output = v
	}
	return p
}

func (u Usage) Cost(p Pricing) float64 {
	return (float64(u.InputTokens)*p.Input +
		float64(u.CacheWriteTokens)*p.CacheWrite +
		float64(u.CacheReadTokens)*p.CacheRead +
		float64(u.OutputTokens)*p.Output) / 1_000_000
}

type Provider interface {
	Name() string
	Model() string
	Judge(ctx context.Context, req Request) ([]Answer, Usage, error)
}
