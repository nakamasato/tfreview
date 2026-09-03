package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLevelOrder(t *testing.T) {
	require.Equal(t, LevelCritical, MaxLevel(LevelHigh, LevelCritical))
	require.Equal(t, LevelHigh, MaxLevel(LevelHigh, LevelNone))
	require.True(t, LevelAtLeast(LevelHigh, LevelMedium))
	require.False(t, LevelAtLeast(LevelMedium, LevelHigh))
}

func TestParseLevel(t *testing.T) {
	l, err := ParseLevel("high")
	require.NoError(t, err)
	require.Equal(t, LevelHigh, l)
	_, err = ParseLevel("severe")
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
