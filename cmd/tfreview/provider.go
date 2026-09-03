package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/nakamasato/tfreview/internal/config"
	"github.com/nakamasato/tfreview/internal/llm"
	"github.com/nakamasato/tfreview/internal/llm/anthropic"
	"github.com/nakamasato/tfreview/internal/llm/mock"
	"github.com/nakamasato/tfreview/internal/model"
)

func newProvider(cfg *config.Config) (llm.Provider, error) {
	switch cfg.LLM.Provider {
	case "anthropic":
		return anthropic.New(anthropic.Options{Model: cfg.LLM.Model, MaxPlanChars: cfg.LLM.MaxPlanChars, APIKey: os.Getenv("ANTHROPIC_API_KEY")}), nil
	case "mock":
		// mock returns fixed verdicts without calling any LLM; gating it behind an
		// explicit opt-in keeps a config typo (or a copied test config) from
		// silently producing fake verdicts in a real run.
		if os.Getenv("TFREVIEW_ALLOW_MOCK") != "1" {
			return nil, fmt.Errorf("llm.provider mock requires TFREVIEW_ALLOW_MOCK=1")
		}
		return mockFromEnv()
	}
	return nil, fmt.Errorf("unsupported provider %q", cfg.LLM.Provider)
}

// mock is for CLI end-to-end tests. Answers are read from the TFREVIEW_MOCK_ANSWERS JSON.
func mockFromEnv() (llm.Provider, error) {
	p := &mock.Provider{Answers: map[string][]llm.Answer{}}
	raw := os.Getenv("TFREVIEW_MOCK_ANSWERS")
	if raw == "" {
		return p, nil
	}
	var in map[string][]struct {
		CheckID string `json:"check_id"`
		Verdict string `json:"verdict"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return nil, fmt.Errorf("TFREVIEW_MOCK_ANSWERS: %w", err)
	}
	for target, answers := range in {
		for _, a := range answers {
			p.Answers[target] = append(p.Answers[target], llm.Answer{CheckID: a.CheckID, Kind: model.VerdictKind(a.Verdict), Reason: a.Reason})
		}
	}
	return p, nil
}
