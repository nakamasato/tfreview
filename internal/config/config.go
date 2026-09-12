// Package config reads .tfreview.yaml and converts it into judgment criteria.
package config

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/nakamasato/tfreview/internal/model"
)

//go:embed default.yaml
var defaultYAML []byte

type Error struct{ Msg string }

func (e *Error) Error() string { return "invalid config: " + e.Msg }

func errorf(format string, a ...any) error { return &Error{Msg: fmt.Sprintf(format, a...)} }

type LLM struct {
	Provider      string             `yaml:"provider"`
	Model         string             `yaml:"model"`
	MaxPlanChars  int                `yaml:"max_plan_chars"`
	MaxDiffChars  int                `yaml:"max_diff_chars"`
	MaxPRChars    int                `yaml:"max_pr_chars"`
	MaxInputChars int                `yaml:"max_input_chars"`
	MaxTokens     int                `yaml:"max_tokens"`
	Pricing       map[string]float64 `yaml:"pricing"`
}

type Config struct {
	Language    string
	LLM         LLM
	Aspects     []model.Aspect
	Checkpoints map[string][]model.Checkpoint
	Digest      string
}

type rawCheck struct {
	ID             string         `yaml:"id"`
	Severity       string         `yaml:"severity"`
	Level          string         `yaml:"level"` // legacy; only to produce a migration error
	Match          map[string]any `yaml:"match"`
	VerdictOnMatch string         `yaml:"verdict_on_match"`
	Question       string         `yaml:"question"`
	Requires       []string       `yaml:"requires"`
}

type rawAspect struct {
	ID     string     `yaml:"id"`
	Title  string     `yaml:"title"`
	Checks []rawCheck `yaml:"checks"`
}

type rawCheckpoint struct {
	ID         string   `yaml:"id"`
	Aspect     string   `yaml:"aspect"`
	Severity   string   `yaml:"severity"`
	Guidance   string   `yaml:"guidance"`
	Requires   []string `yaml:"requires"`
	References []string `yaml:"references"`
}

type rawConfig struct {
	Language    string                     `yaml:"language"`
	LLM         LLM                        `yaml:"llm"`
	Aspects     *[]rawAspect               `yaml:"aspects"`
	Categories  *[]rawAspect               `yaml:"categories"` // legacy
	Checkpoints map[string][]rawCheckpoint `yaml:"checkpoints_for_resource"`
}

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

func Parse(raw []byte) (*Config, error) {
	var rc rawConfig
	if err := yaml.Unmarshal(raw, &rc); err != nil {
		return nil, errorf("%v", err)
	}
	if rc.Categories != nil {
		return nil, errorf("categories: has been renamed to aspects:")
	}

	digestInput := raw
	if rc.Aspects == nil && rc.Checkpoints == nil {
		var def rawConfig
		if err := yaml.Unmarshal(defaultYAML, &def); err != nil {
			return nil, fmt.Errorf("builtin default.yaml is broken: %w", err)
		}
		rc.Aspects = def.Aspects
		rc.Checkpoints = def.Checkpoints
		// Mix the builtin defaults into the digest so state is invalidated when they change too.
		digestInput = append(append([]byte{}, raw...), defaultYAML...)
	}

	c := &Config{Language: rc.Language, LLM: rc.LLM}
	if c.Language == "" {
		c.Language = "en"
	}
	if c.LLM.Provider == "" {
		c.LLM.Provider = "anthropic"
	}
	if c.LLM.Provider != "anthropic" && c.LLM.Provider != "mock" {
		return nil, errorf("llm.provider %q is not supported (anthropic|mock)", c.LLM.Provider)
	}
	if c.LLM.Model == "" {
		c.LLM.Model = "claude-opus-5"
	}
	if c.LLM.MaxPlanChars == 0 {
		c.LLM.MaxPlanChars = 100000
	}
	if c.LLM.MaxDiffChars == 0 {
		c.LLM.MaxDiffChars = 60000
	}
	if c.LLM.MaxPRChars == 0 {
		c.LLM.MaxPRChars = 8000
	}
	if c.LLM.MaxInputChars == 0 {
		c.LLM.MaxInputChars = 160000
	}
	if c.LLM.MaxTokens == 0 {
		c.LLM.MaxTokens = 128000
	}

	if rc.Aspects == nil || len(*rc.Aspects) == 0 {
		return nil, errorf("aspects must not be empty")
	}
	seenAspect := map[string]bool{}
	seenCheck := map[string]bool{}
	for _, rasp := range *rc.Aspects {
		if rasp.ID == "" || seenAspect[rasp.ID] {
			return nil, errorf("aspect id %q is empty or duplicated", rasp.ID)
		}
		seenAspect[rasp.ID] = true
		asp := model.Aspect{ID: rasp.ID, Title: rasp.Title}
		if asp.Title == "" {
			asp.Title = asp.ID
		}
		for _, rck := range rasp.Checks {
			ck, err := convertCheck(rck)
			if err != nil {
				return nil, err
			}
			if seenCheck[ck.ID] {
				return nil, errorf("check id %q is duplicated", ck.ID)
			}
			seenCheck[ck.ID] = true
			asp.Checks = append(asp.Checks, ck)
		}
		c.Aspects = append(c.Aspects, asp)
	}

	if len(rc.Checkpoints) > 0 {
		c.Checkpoints = map[string][]model.Checkpoint{}
		for _, resourceType := range slices.Sorted(maps.Keys(rc.Checkpoints)) {
			if resourceType == "" {
				return nil, errorf("checkpoints_for_resource: resource type must not be empty")
			}
			for _, rcp := range rc.Checkpoints[resourceType] {
				cp, err := convertCheckpoint(resourceType, rcp)
				if err != nil {
					return nil, err
				}
				if !seenAspect[cp.Aspect] {
					return nil, errorf("checkpoint %q: unknown aspect %q", cp.ID, cp.Aspect)
				}
				if seenCheck[cp.ID] {
					return nil, errorf("checkpoint id %q is duplicated", cp.ID)
				}
				seenCheck[cp.ID] = true
				c.Checkpoints[resourceType] = append(c.Checkpoints[resourceType], cp)
			}
		}
	}

	sum := sha256.Sum256(digestInput)
	c.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return c, nil
}

func convertCheck(r rawCheck) (model.Check, error) {
	if r.ID == "" {
		return model.Check{}, errorf("check id must not be empty")
	}
	if r.Level != "" {
		return model.Check{}, errorf("check %q: level: has been renamed to severity:", r.ID)
	}
	severity, err := model.ParseSeverity(r.Severity)
	if err != nil {
		return model.Check{}, errorf("check %q: %v", r.ID, err)
	}
	if err := validateRequires(r.ID, r.Requires); err != nil {
		return model.Check{}, err
	}
	m, err := convertMatch(r.ID, r.Match)
	if err != nil {
		return model.Check{}, err
	}
	on := model.OnMatch(r.VerdictOnMatch)
	if on == "" {
		on = model.OnMatchHit
	}
	switch on {
	case model.OnMatchHit, model.OnMatchAsk, model.OnMatchUnverifiable:
	default:
		return model.Check{}, errorf("check %q: unknown verdict_on_match %q (hit|ask|unverifiable)", r.ID, r.VerdictOnMatch)
	}
	if m.IsZero() && r.Question == "" {
		return model.Check{}, errorf("check %q: needs at least one of match or question", r.ID)
	}
	if m.IsZero() && on != model.OnMatchHit {
		return model.Check{}, errorf("check %q: verdict_on_match %q requires match", r.ID, on)
	}
	if on == model.OnMatchAsk && r.Question == "" {
		return model.Check{}, errorf("check %q: verdict_on_match ask requires question", r.ID)
	}
	if !m.IsZero() && (on == model.OnMatchHit || on == model.OnMatchUnverifiable) && r.Question != "" {
		return model.Check{}, errorf("check %q: question has no effect with verdict_on_match %q; use ask or remove the question", r.ID, on)
	}
	return model.Check{ID: r.ID, Severity: severity, Match: m, OnMatch: on, Question: r.Question, Requires: r.Requires}, nil
}

func convertCheckpoint(resourceType string, r rawCheckpoint) (model.Checkpoint, error) {
	if r.ID == "" {
		return model.Checkpoint{}, errorf("checkpoints_for_resource %q: checkpoint id must not be empty", resourceType)
	}
	if r.Aspect == "" {
		return model.Checkpoint{}, errorf("checkpoint %q: aspect is required", r.ID)
	}
	sev, err := model.ParseSeverity(r.Severity)
	if err != nil || sev == model.SeverityNone {
		return model.Checkpoint{}, errorf("checkpoint %q: severity must be medium, high or critical", r.ID)
	}
	if strings.TrimSpace(r.Guidance) == "" {
		return model.Checkpoint{}, errorf("checkpoint %q: guidance must not be empty", r.ID)
	}
	if err := validateRequires(r.ID, r.Requires); err != nil {
		return model.Checkpoint{}, err
	}
	for _, ref := range r.References {
		if !strings.HasPrefix(ref, "http://") && !strings.HasPrefix(ref, "https://") {
			return model.Checkpoint{}, errorf("checkpoint %q: reference %q must be an http(s) URL", r.ID, ref)
		}
	}
	return model.Checkpoint{
		ID: r.ID, Aspect: r.Aspect, Severity: sev, Guidance: r.Guidance,
		Requires: r.Requires, References: r.References,
	}, nil
}

func validateRequires(id string, reqs []string) error {
	for _, req := range reqs {
		if req != model.RequiresDiff && req != model.RequiresPR {
			return errorf("check %q: unknown requires %q (diff|pr)", id, req)
		}
	}
	return nil
}

func convertMatch(id string, raw map[string]any) (model.Match, error) {
	var m model.Match
	for k, v := range raw {
		list, err := stringList(v)
		if err != nil {
			return m, errorf("check %q: match.%s must be a list of strings", id, k)
		}
		switch k {
		case "actions":
			m.Actions = list
		case "types":
			m.Types = list
		case "targets":
			m.Targets = list
		default:
			return m, errorf("check %q: unknown match key %q (actions|types|targets)", id, k)
		}
	}
	return m, nil
}

func stringList(v any) ([]string, error) {
	items, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("not a list")
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		s, ok := it.(string)
		if !ok {
			return nil, fmt.Errorf("not a string")
		}
		out = append(out, s)
	}
	return out, nil
}

func (c *Config) Checks() []model.Check {
	var out []model.Check
	for _, asp := range c.Aspects {
		out = append(out, asp.Checks...)
	}
	return out
}

func (c *Config) Check(id string) (model.Check, bool) {
	for _, ck := range c.Checks() {
		if ck.ID == id {
			return ck, true
		}
	}
	return model.Check{}, false
}

func (c *Config) AspectOf(checkID string) (model.Aspect, bool) {
	for _, asp := range c.Aspects {
		for _, ck := range asp.Checks {
			if ck.ID == checkID {
				return asp, true
			}
		}
	}
	return model.Aspect{}, false
}

func (c *Config) CheckpointsFor(resourceType string) []model.Checkpoint {
	return c.Checkpoints[resourceType]
}

func (c *Config) Checkpoint(id string) (model.Checkpoint, bool) {
	for _, cps := range c.Checkpoints {
		for _, cp := range cps {
			if cp.ID == id {
				return cp, true
			}
		}
	}
	return model.Checkpoint{}, false
}
