package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nakamasato/tfreview/internal/model"
	"github.com/stretchr/testify/require"
)

func TestLoadMissingOrBrokenIsEmpty(t *testing.T) {
	require.Empty(t, Load("").Targets)
	require.Empty(t, Load("/nonexistent").Targets)
	p := filepath.Join(t.TempDir(), "s.json")
	require.NoError(t, os.WriteFile(p, []byte("{"), 0o644))
	require.Empty(t, Load(p).Targets)
}

func TestPutAndReusable(t *testing.T) {
	s := New("abc", "cfg1")
	vs := []model.Verdict{{CheckID: "a", Kind: model.VerdictHit, Reason: "r", Source: model.SourceLLM}}
	s.Put("prd", "plan1", vs)

	got, ok := s.Reusable("prd", "plan1", "cfg1")
	require.True(t, ok)
	require.Equal(t, vs, got)

	_, ok = s.Reusable("prd", "plan2", "cfg1")
	require.False(t, ok)
	_, ok = s.Reusable("prd", "plan1", "cfg2")
	require.False(t, ok)
	_, ok = s.Reusable("dev", "plan1", "cfg1")
	require.False(t, ok)
}

func TestPutSkipsUnevaluated(t *testing.T) {
	s := New("abc", "cfg1")
	s.Put("prd", "plan1", []model.Verdict{{CheckID: "a", Kind: model.VerdictSkipped}})
	require.Empty(t, s.Targets)
}

func TestSaveLoadRoundTrip(t *testing.T) {
	s := New("abc", "cfg1")
	s.Put("prd", "plan1", []model.Verdict{{CheckID: "a", Kind: model.VerdictMiss, Reason: "", Source: model.SourceLLM}})
	p := filepath.Join(t.TempDir(), "s.json")
	require.NoError(t, s.Save(p))
	got := Load(p)
	require.Equal(t, "abc", got.HeadSHA)
	require.Equal(t, "cfg1", got.ConfigDigest)
	_, ok := got.Reusable("prd", "plan1", "cfg1")
	require.True(t, ok)
}
