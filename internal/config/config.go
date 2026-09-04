// Package config reads .tfreview.yaml and converts it into judgment criteria.
package config

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/goccy/go-yaml"
	"github.com/nakamasato/tfreview/internal/model"
)

//go:embed default.yaml
var defaultYAML []byte

type Error struct{ Msg string }

func (e *Error) Error() string { return "invalid config: " + e.Msg }

func errorf(format string, a ...any) error { return &Error{Msg: fmt.Sprintf(format, a...)} }

type LLM struct {
	Provider     string             `yaml:"provider"`
	Model        string             `yaml:"model"`
	MaxPlanChars int                `yaml:"max_plan_chars"`
	MaxTokens    int                `yaml:"max_tokens"`
	Pricing      map[string]float64 `yaml:"pricing"`
}

type Config struct {
	Language   string
	LLM        LLM
	Categories []model.Category
	Digest     string
}

type rawCheck struct {
	ID             string         `yaml:"id"`
	Level          string         `yaml:"level"`
	Match          map[string]any `yaml:"match"`
	VerdictOnMatch string         `yaml:"verdict_on_match"`
	Question       string         `yaml:"question"`
}

type rawCategory struct {
	ID     string     `yaml:"id"`
	Title  string     `yaml:"title"`
	Checks []rawCheck `yaml:"checks"`
}

type rawConfig struct {
	Language   string         `yaml:"language"`
	LLM        LLM            `yaml:"llm"`
	Categories *[]rawCategory `yaml:"categories"`
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
	digestInput := raw
	if rc.Categories == nil {
		var def rawConfig
		if err := yaml.Unmarshal(defaultYAML, &def); err != nil {
			return nil, fmt.Errorf("builtin default.yaml is broken: %w", err)
		}
		rc.Categories = def.Categories
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
	if c.LLM.MaxTokens == 0 {
		c.LLM.MaxTokens = 128000
	}

	if len(*rc.Categories) == 0 {
		return nil, errorf("categories must not be empty")
	}
	seenCat := map[string]bool{}
	seenCheck := map[string]bool{}
	for _, rcat := range *rc.Categories {
		if rcat.ID == "" || seenCat[rcat.ID] {
			return nil, errorf("category id %q is empty or duplicated", rcat.ID)
		}
		seenCat[rcat.ID] = true
		cat := model.Category{ID: rcat.ID, Title: rcat.Title}
		if cat.Title == "" {
			cat.Title = cat.ID
		}
		for _, rck := range rcat.Checks {
			ck, err := convertCheck(rck)
			if err != nil {
				return nil, err
			}
			if seenCheck[ck.ID] {
				return nil, errorf("check id %q is duplicated", ck.ID)
			}
			seenCheck[ck.ID] = true
			cat.Checks = append(cat.Checks, ck)
		}
		c.Categories = append(c.Categories, cat)
	}

	sum := sha256.Sum256(digestInput)
	c.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return c, nil
}

func convertCheck(r rawCheck) (model.Check, error) {
	if r.ID == "" {
		return model.Check{}, errorf("check id must not be empty")
	}
	level, err := model.ParseLevel(r.Level)
	if err != nil {
		return model.Check{}, errorf("check %q: %v", r.ID, err)
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
	return model.Check{ID: r.ID, Level: level, Match: m, OnMatch: on, Question: r.Question}, nil
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
	for _, cat := range c.Categories {
		out = append(out, cat.Checks...)
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

func (c *Config) CategoryOf(checkID string) (model.Category, bool) {
	for _, cat := range c.Categories {
		for _, ck := range cat.Checks {
			if ck.ID == checkID {
				return cat, true
			}
		}
	}
	return model.Category{}, false
}
