package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func run(t *testing.T, args ...string) error {
	t.Helper()
	cmd := newRootCmd()
	cmd.SetArgs(args)
	return cmd.Execute()
}

func runCapture(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := newRootCmd()
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return outBuf.String(), errBuf.String(), err
}

func extractFixture(t *testing.T, dir, target string) string {
	t.Helper()
	out := filepath.Join(dir, target+".json")
	require.NoError(t, run(t, "extract", "--show-json", filepath.Join("..", "..", "testdata", "show-basic.json"), "--target", target, "--out", out))
	return out
}

func writeCfg(t *testing.T, dir, body string) string {
	t.Helper()
	p := filepath.Join(dir, ".tfreview.yaml")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	return p
}

const mockCfg = `
llm: {provider: mock}
categories:
  - id: destruction
    title: D
    checks:
      - {id: delete-or-replace, level: critical, match: {actions: [delete]}, verdict_on_match: ask, question: q}
      - {id: llm-only, level: high, question: q}
`

func TestExtractWritesPlan(t *testing.T) {
	dir := t.TempDir()
	out := extractFixture(t, dir, "prd")
	b, err := os.ReadFile(out)
	require.NoError(t, err)
	require.Contains(t, string(b), `"target": "prd"`)
	require.Contains(t, string(b), `"changed_keys"`)
}

func TestExtractFromStdin(t *testing.T) {
	dir := t.TempDir()
	f, err := os.Open(filepath.Join("..", "..", "testdata", "show-basic.json"))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	cmd := newRootCmd()
	cmd.SetIn(f)
	cmd.SetArgs([]string{"extract", "--show-json", "-", "--target", "x", "--out", filepath.Join(dir, "x.json")})
	require.NoError(t, cmd.Execute())
	require.FileExists(t, filepath.Join(dir, "x.json"))
}

func TestReviewEndToEndWithMock(t *testing.T) {
	now = func() time.Time { return time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { now = time.Now })
	dir := t.TempDir()
	p := extractFixture(t, dir, "prd")
	cfg := writeCfg(t, dir, mockCfg)
	t.Setenv("TFREVIEW_ALLOW_MOCK", "1")
	t.Setenv("TFREVIEW_MOCK_ANSWERS", `{"prd":[{"check_id":"delete-or-replace","verdict":"hit","reason":"db"},{"check_id":"llm-only","verdict":"miss","reason":"ok"}]}`)
	outDir := filepath.Join(dir, "out")

	require.NoError(t, run(t, "review", "--plan", p, "--config", cfg, "--out-dir", outDir, "--head-sha", "abc1234", "--repo", "o/r"))

	label, _ := os.ReadFile(filepath.Join(outDir, "label.txt"))
	require.Equal(t, "tfreview:critical\n", string(label))
	comment, _ := os.ReadFile(filepath.Join(outDir, "comment.md"))
	require.Contains(t, string(comment), "Risk: critical")
	require.Contains(t, string(comment), "2026-09-02T00:00:00Z")
	require.FileExists(t, filepath.Join(outDir, "result.json"))
	require.FileExists(t, filepath.Join(outDir, "state.json"))
}

func TestReviewFailOn(t *testing.T) {
	dir := t.TempDir()
	p := extractFixture(t, dir, "prd")
	cfg := writeCfg(t, dir, mockCfg)
	t.Setenv("TFREVIEW_ALLOW_MOCK", "1")
	t.Setenv("TFREVIEW_MOCK_ANSWERS", `{"prd":[{"check_id":"delete-or-replace","verdict":"hit","reason":"db"},{"check_id":"llm-only","verdict":"hit","reason":"x"}]}`)

	err := run(t, "review", "--plan", p, "--config", cfg, "--out-dir", filepath.Join(dir, "o1"), "--fail-on", "critical")
	require.Equal(t, 1, exitCode(err))

	// rule-only: the LLM answers "hit", so the rule score stays none and doesn't fail
	err = run(t, "review", "--plan", p, "--config", cfg, "--out-dir", filepath.Join(dir, "o2"), "--fail-on", "critical", "--fail-on-rule-only")
	require.NoError(t, err)

	err = run(t, "review", "--plan", p, "--config", cfg, "--out-dir", filepath.Join(dir, "o3"), "--fail-on", "bogus")
	require.Equal(t, 2, exitCode(err))
}

func TestReviewReusesState(t *testing.T) {
	dir := t.TempDir()
	p := extractFixture(t, dir, "prd")
	cfg := writeCfg(t, dir, mockCfg)
	t.Setenv("TFREVIEW_ALLOW_MOCK", "1")
	t.Setenv("TFREVIEW_MOCK_ANSWERS", `{"prd":[{"check_id":"delete-or-replace","verdict":"miss","reason":"no"},{"check_id":"llm-only","verdict":"miss","reason":"no"}]}`)
	o1 := filepath.Join(dir, "o1")
	require.NoError(t, run(t, "review", "--plan", p, "--config", cfg, "--out-dir", o1))

	// The second run passes in state. Even with different mock answers the
	// cached verdict is reused, so the result stays the same.
	t.Setenv("TFREVIEW_MOCK_ANSWERS", `{"prd":[{"check_id":"delete-or-replace","verdict":"hit","reason":"changed"},{"check_id":"llm-only","verdict":"hit","reason":"changed"}]}`)
	o2 := filepath.Join(dir, "o2")
	require.NoError(t, run(t, "review", "--plan", p, "--config", cfg, "--out-dir", o2, "--state-in", filepath.Join(o1, "state.json")))
	c, _ := os.ReadFile(filepath.Join(o2, "comment.md"))
	require.Contains(t, string(c), "reused")
	require.NotContains(t, string(c), "changed")
}

func TestReviewInvalidConfigExit2(t *testing.T) {
	dir := t.TempDir()
	p := extractFixture(t, dir, "prd")
	cfg := writeCfg(t, dir, "categories: []")
	err := run(t, "review", "--plan", p, "--config", cfg, "--out-dir", filepath.Join(dir, "o"))
	require.Equal(t, 2, exitCode(err))
}

func TestReviewNoPlansIsNone(t *testing.T) {
	dir := t.TempDir()
	cfg := writeCfg(t, dir, mockCfg)
	t.Setenv("TFREVIEW_ALLOW_MOCK", "1")
	o := filepath.Join(dir, "o")
	require.NoError(t, run(t, "review", "--config", cfg, "--out-dir", o))
	label, _ := os.ReadFile(filepath.Join(o, "label.txt"))
	require.Equal(t, "tfreview:none\n", string(label))
}

func TestReviewMockProviderRequiresOptIn(t *testing.T) {
	dir := t.TempDir()
	p := extractFixture(t, dir, "prd")
	cfg := writeCfg(t, dir, mockCfg)
	err := run(t, "review", "--plan", p, "--config", cfg, "--out-dir", filepath.Join(dir, "o"))
	require.Equal(t, 2, exitCode(err))
}

func TestReviewIncompleteSummarizesSkippedChecks(t *testing.T) {
	dir := t.TempDir()
	p := extractFixture(t, dir, "prd")
	cfg := writeCfg(t, dir, mockCfg)
	t.Setenv("TFREVIEW_ALLOW_MOCK", "1")
	t.Setenv("TFREVIEW_MOCK_ANSWERS", `{}`)
	outDir := filepath.Join(dir, "out")

	_, stderr, err := runCapture(t, "review", "--plan", p, "--config", cfg, "--out-dir", outDir)
	require.NoError(t, err)
	require.Contains(t, stderr, "skipped:")
	require.Contains(t, stderr, "llm-only")
	require.Contains(t, stderr, "no answer returned")
}

func TestReviewWarnsOnUnmatchedMatchTargets(t *testing.T) {
	dir := t.TempDir()
	p := extractFixture(t, dir, "dev")
	cfg := writeCfg(t, dir, `
llm: {provider: mock}
categories:
  - id: destruction
    title: D
    checks:
      - {id: prod-only, level: critical, match: {targets: [prod]}, verdict_on_match: ask, question: q}
`)
	t.Setenv("TFREVIEW_ALLOW_MOCK", "1")
	t.Setenv("TFREVIEW_MOCK_ANSWERS", `{}`)
	outDir := filepath.Join(dir, "out")

	_, stderr, err := runCapture(t, "review", "--plan", p, "--config", cfg, "--out-dir", outDir)
	require.NoError(t, err)
	require.Contains(t, stderr, `match.targets "prod"`)
	require.Contains(t, stderr, "prod-only")
	require.Contains(t, stderr, "loaded targets: dev")
	require.Contains(t, stderr, "check for a typo")
}

func TestReviewWarnsWhenAnthropicKeyMissing(t *testing.T) {
	dir := t.TempDir()
	p := extractFixture(t, dir, "prd")
	cfg := writeCfg(t, dir, "llm: {provider: anthropic}\ncategories:\n  - id: destruction\n    title: D\n    checks:\n      - {id: llm-only, level: high, question: q}\n")
	t.Setenv("ANTHROPIC_API_KEY", "")
	outDir := filepath.Join(dir, "out")

	_, stderr, err := runCapture(t, "review", "--plan", p, "--config", cfg, "--out-dir", outDir)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode(err))
	require.Contains(t, stderr, "ANTHROPIC_API_KEY is not set")

	label, readErr := os.ReadFile(filepath.Join(outDir, "label.txt"))
	require.NoError(t, readErr)
	require.Equal(t, "tfreview:unknown\n", string(label))
}
