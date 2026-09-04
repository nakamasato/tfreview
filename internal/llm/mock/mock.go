// Package mock is a Provider for use in tests.
package mock

import (
	"context"

	"github.com/nakamasato/tfreview/internal/llm"
)

type Provider struct {
	Answers map[string][]llm.Answer
	Err     error
	Calls   []llm.Request
}

func (p *Provider) Name() string  { return "mock" }
func (p *Provider) Model() string { return "mock-model" }

func (p *Provider) Judge(_ context.Context, req llm.Request) ([]llm.Answer, llm.Usage, error) {
	p.Calls = append(p.Calls, req)
	if p.Err != nil {
		return nil, llm.Usage{}, p.Err
	}
	return p.Answers[req.Plan.Target], llm.Usage{Calls: 1, InputTokens: 1000, OutputTokens: 100}, nil
}
