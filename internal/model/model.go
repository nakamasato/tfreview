// Package model defines the vocabulary for verdicts (severities, verdicts, checks).
package model

import (
	"fmt"
	"slices"
)

type Severity string

const (
	SeverityNone     Severity = "none"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

var severityRank = map[Severity]int{SeverityNone: 0, SeverityMedium: 1, SeverityHigh: 2, SeverityCritical: 3}

func ParseSeverity(s string) (Severity, error) {
	sv := Severity(s)
	if _, ok := severityRank[sv]; !ok {
		return "", fmt.Errorf("unknown severity %q (want none|medium|high|critical)", s)
	}
	return sv, nil
}

func (s Severity) Rank() int { return severityRank[s] }

func MaxSeverity(a, b Severity) Severity {
	if b.Rank() > a.Rank() {
		return b
	}
	return a
}

func SeverityAtLeast(s, threshold Severity) bool { return s.Rank() >= threshold.Rank() }

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
	CheckID   string      `json:"check_id"`
	Kind      VerdictKind `json:"verdict"`
	Reason    string      `json:"reason"`
	Source    Source      `json:"source"`
	Severity  Severity    `json:"severity,omitempty"`
	Resources []string    `json:"resources,omitempty"`
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

const (
	RequiresDiff = "diff"
	RequiresPR   = "pr"
)

func HasRequirement(reqs []string, req string) bool { return slices.Contains(reqs, req) }

// Criteria bounds a proposition by saying what puts it on each side. It exists because
// a probability-only judge has nowhere to record an exception it noticed, so every
// exclusion has to be stated up front instead of left to the judge's discretion.
type Criteria struct {
	True  string
	False string
}

type Check struct {
	ID       string
	Severity Severity
	Match    Match
	OnMatch  OnMatch
	// Question is prose addressed to a judge that answers in prose.
	Question string
	// Instructions is the same check as a proposition a judge can only agree or
	// disagree with, scored rather than answered.
	Instructions string
	Criteria     *Criteria
	Requires     []string
}

type Aspect struct {
	ID     string
	Title  string
	Checks []Check
}

// Checkpoint is knowledge attached to a resource type. It is always LLM-judged:
// Severity is what the config author declared for the class of problem, while the
// Severity on a resulting Verdict is what the model judged for this plan.
type Checkpoint struct {
	ID         string
	Aspect     string
	Severity   Severity
	Guidance   string
	Requires   []string
	References []string
}
