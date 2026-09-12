package judge

import (
	"testing"

	"github.com/nakamasato/tfreview/internal/model"
	"github.com/stretchr/testify/require"
)

func TestMergeKeepsMostDangerous(t *testing.T) {
	v := Merge([]model.Verdict{
		{CheckID: "a", Kind: model.VerdictMiss, Reason: "m"},
		{CheckID: "a", Kind: model.VerdictHit, Reason: "h"},
		{CheckID: "a", Kind: model.VerdictSkipped, Reason: "s"},
	})
	require.Equal(t, model.VerdictHit, v.Kind)
	require.Contains(t, v.Reason, "h")
	require.Contains(t, v.Reason, "not evaluated")
	require.NotContains(t, v.Reason, "unverifiable")
}

func TestMergeNoteUnverifiable(t *testing.T) {
	v := Merge([]model.Verdict{
		{Kind: model.VerdictUnverifiable, Reason: "u"},
		{Kind: model.VerdictMiss, Reason: "m"},
	})
	require.Equal(t, model.VerdictUnverifiable, v.Kind)
	require.Equal(t, "u", v.Reason)
}

func TestMergeSingle(t *testing.T) {
	v := Merge([]model.Verdict{{Kind: model.VerdictMiss, Reason: "m"}})
	require.Equal(t, "m", v.Reason)
}

func TestMergeSkipsNoteMatchingWinnerKind(t *testing.T) {
	v := Merge([]model.Verdict{
		{Kind: model.VerdictSkipped, Reason: "llm unavailable"},
		{Kind: model.VerdictSkipped, Reason: "llm unavailable"},
	})
	require.Equal(t, model.VerdictSkipped, v.Kind)
	require.Equal(t, "llm unavailable", v.Reason)
	require.NotContains(t, v.Reason, "other targets")
}

func TestMergeKeepsNoteForDifferentLoserKind(t *testing.T) {
	v := Merge([]model.Verdict{
		{Kind: model.VerdictHit, Reason: "h"},
		{Kind: model.VerdictSkipped, Reason: "s"},
	})
	require.Equal(t, model.VerdictHit, v.Kind)
	require.Contains(t, v.Reason, "not evaluated")
}
