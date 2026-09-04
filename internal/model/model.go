// Package model defines the vocabulary for verdicts (levels, verdicts, checks).
package model

import "fmt"

type Level string

const (
	LevelNone     Level = "none"
	LevelMedium   Level = "medium"
	LevelHigh     Level = "high"
	LevelCritical Level = "critical"
)

var levelRank = map[Level]int{LevelNone: 0, LevelMedium: 1, LevelHigh: 2, LevelCritical: 3}

func ParseLevel(s string) (Level, error) {
	l := Level(s)
	if _, ok := levelRank[l]; !ok {
		return "", fmt.Errorf("unknown level %q (want none|medium|high|critical)", s)
	}
	return l, nil
}

func (l Level) Rank() int { return levelRank[l] }

func MaxLevel(a, b Level) Level {
	if b.Rank() > a.Rank() {
		return b
	}
	return a
}

func LevelAtLeast(l, threshold Level) bool { return l.Rank() >= threshold.Rank() }

type VerdictKind string

const (
	VerdictHit          VerdictKind = "hit"
	VerdictMiss         VerdictKind = "miss"
	VerdictUnverifiable VerdictKind = "unverifiable"
	VerdictSkipped      VerdictKind = "skipped"
)

// skipped is at the lowest rank because "could not evaluate" is weaker information
// than "no match" (miss). When merging across targets, skipped should not override miss.
var verdictRank = map[VerdictKind]int{VerdictSkipped: 0, VerdictMiss: 1, VerdictUnverifiable: 2, VerdictHit: 3}

func (v VerdictKind) Rank() int { return verdictRank[v] }

type Source string

const (
	SourceRule Source = "rule"
	SourceLLM  Source = "llm"
)

type Verdict struct {
	CheckID string      `json:"check_id"`
	Kind    VerdictKind `json:"verdict"`
	Reason  string      `json:"reason"`
	Source  Source      `json:"source"`
}

type Match struct {
	Actions []string `json:"actions,omitempty" yaml:"actions"`
	Types   []string `json:"types,omitempty" yaml:"types"`
	Targets []string `json:"targets,omitempty" yaml:"targets"`
}

func (m Match) IsZero() bool {
	return len(m.Actions) == 0 && len(m.Types) == 0 && len(m.Targets) == 0
}

type OnMatch string

const (
	OnMatchHit          OnMatch = "hit"
	OnMatchAsk          OnMatch = "ask"
	OnMatchUnverifiable OnMatch = "unverifiable"
)

type Check struct {
	ID       string
	Level    Level
	Match    Match
	OnMatch  OnMatch
	Question string
}

type Category struct {
	ID     string
	Title  string
	Checks []Check
}
