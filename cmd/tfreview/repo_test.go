package main

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseGitHubRemote(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want string
		ok   bool
	}{
		{"https", "https://github.com/acme/widgets", "acme/widgets", true},
		{"https with .git", "https://github.com/acme/widgets.git", "acme/widgets", true},
		{"scp-like", "git@github.com:acme/widgets.git", "acme/widgets", true},
		{"scp-like no .git", "git@github.com:acme/widgets", "acme/widgets", true},
		{"ssh scheme", "ssh://git@github.com/acme/widgets.git", "acme/widgets", true},
		{"other host", "https://gitlab.com/acme/widgets.git", "", false},
		{"malformed", "not a url", "", false},
		{"empty", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parseGitHubRemote(c.url)
			require.Equal(t, c.ok, ok)
			require.Equal(t, c.want, got)
		})
	}
}

func initGitRepoWithRemote(t *testing.T, remote string) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "git %v: %s", args, out)
	}
	run("init")
	if remote != "" {
		run("remote", "add", "origin", remote)
	}
	return dir
}

func TestGitRemoteRepoFromTempClone(t *testing.T) {
	dir := initGitRepoWithRemote(t, "git@github.com:acme/widgets.git")
	t.Chdir(dir)
	require.Equal(t, "acme/widgets", gitRemoteRepo())
}

func TestGitRemoteRepoNoRemote(t *testing.T) {
	dir := initGitRepoWithRemote(t, "")
	t.Chdir(dir)
	require.Equal(t, "", gitRemoteRepo())
}
