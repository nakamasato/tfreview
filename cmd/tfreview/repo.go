package main

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
)

var githubRemoteRe = regexp.MustCompile(`^(?:https://github\.com/|git@github\.com:|ssh://git@github\.com/)([^/]+/[^/]+?)(?:\.git)?/?$`)

// parseGitHubRemote は github.com の origin URL から owner/repo を取り出す。
// https/scp-like/ssh の各形式に対応し、他ホストや不正な形式は false を返す。
func parseGitHubRemote(url string) (string, bool) {
	url = strings.TrimSpace(url)
	m := githubRemoteRe.FindStringSubmatch(url)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// gitRemoteRepo は `git remote get-url origin` の結果から github.com のリポジトリを推測する。
// git が無い・リポジトリでない・origin が無い・github.com 以外、いずれも空文字を返す
// (gh のカレントディレクトリからの推測と同じふるまい)。
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
