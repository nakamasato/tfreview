package main

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
)

var githubRemoteRe = regexp.MustCompile(`^(?:https://github\.com/|git@github\.com:|ssh://git@github\.com/)([^/]+/[^/]+?)(?:\.git)?/?$`)

// parseGitHubRemote extracts owner/repo from a github.com origin URL.
// It handles the https/scp-like/ssh forms and returns false for other hosts or malformed input.
func parseGitHubRemote(url string) (string, bool) {
	url = strings.TrimSpace(url)
	m := githubRemoteRe.FindStringSubmatch(url)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// gitRemoteRepo infers the github.com repository from `git remote get-url origin`.
// It returns an empty string if git is missing, this isn't a repo, there's no
// origin, or origin isn't github.com (matching gh's own current-directory inference).
func gitRemoteRepo() string {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	repo, ok := parseGitHubRemote(string(out))
	if !ok {
		return ""
	}
	return repo
}

func resolveRepo(flag string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	if v := os.Getenv("GITHUB_REPOSITORY"); v != "" {
		return v, nil
	}
	if v := gitRemoteRepo(); v != "" {
		return v, nil
	}
	return "", &exitError{code: 2, msg: "--repo is required (set it, GITHUB_REPOSITORY, or run inside a clone with a github.com origin)"}
}
