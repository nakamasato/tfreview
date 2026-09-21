package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSeverityOrder(t *testing.T) {
	require.Equal(t, SeverityCritical, MaxSeverity(SeverityHigh, SeverityCritical))
	require.Equal(t, SeverityHigh, MaxSeverity(SeverityHigh, SeverityNone))
	require.True(t, SeverityAtLeast(SeverityHigh, SeverityMedium))
	require.False(t, SeverityAtLeast(SeverityMedium, SeverityHigh))
}

func TestParseSeverity_High(t *testing.T) {
	s, err := ParseSeverity("high")
	require.NoError(t, err)
	require.Equal(t, SeverityHigh, s)
	_, err = ParseSeverity("severe")
	require.Error(t, err)
}

func TestVerdictRank(t *testing.T) {
	require.Greater(t, VerdictHit.Rank(), VerdictUnverifiable.Rank())
	require.Greater(t, VerdictUnverifiable.Rank(), VerdictMiss.Rank())
	require.Greater(t, VerdictMiss.Rank(), VerdictSkipped.Rank())
}

func TestMatchIsZero(t *testing.T) {
	require.True(t, Match{}.IsZero())
	require.False(t, Match{Actions: []string{"delete"}}.IsZero())
}

func TestParseSeverity(t *testing.T) {
	for _, s := range []string{"none", "medium", "high", "critical"} {
		if _, err := ParseSeverity(s); err != nil {
			t.Errorf("ParseSeverity(%q) returned error: %v", s, err)
		}
	}
	if _, err := ParseSeverity("bogus"); err == nil {
		t.Error("ParseSeverity(\"bogus\") returned no error")
	}
}

func TestMaxSeverity(t *testing.T) {
	if got := MaxSeverity(SeverityMedium, SeverityCritical); got != SeverityCritical {
		t.Errorf("MaxSeverity = %q, want critical", got)
	}
}

func TestHasRequirement(t *testing.T) {
	if !HasRequirement([]string{"diff"}, RequiresDiff) {
		t.Error("HasRequirement did not find diff")
	}
	if HasRequirement(nil, RequiresDiff) {
		t.Error("HasRequirement found diff in an empty list")
	}
}

func TestCheckpointZeroValue(t *testing.T) {
	cp := Checkpoint{ID: "x", Aspect: "data-loss", Severity: SeverityHigh, Guidance: "look"}
	if cp.Severity.Rank() != SeverityHigh.Rank() {
		t.Errorf("Severity rank = %d, want %d", cp.Severity.Rank(), SeverityHigh.Rank())
	}
}

func TestCheckProse(t *testing.T) {
	ck := Check{
		Instructions: "  The change at `focus` deletes a database.\n",
		Criteria:     &Criteria{True: "the action is destroy", False: "anything else"},
	}
	require.Equal(t,
		"The change at `focus` deletes a database. This holds when the action is destroy. It does not hold for anything else.",
		ck.Prose())
}

func TestCheckProseWithoutCriteria(t *testing.T) {
	require.Equal(t, "It deletes a database.", Check{Instructions: "It deletes a database.\n"}.Prose())
}
