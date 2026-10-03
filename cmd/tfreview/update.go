package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const latestReleaseURL = "https://api.github.com/repos/nakamasato/tfreview/releases/latest"

// latestRelease returns the latest stable release when it is newer than the
// running version. Update checks are best-effort and deliberately skipped in CI.
func latestRelease(ctx context.Context, current string) (string, bool) {
	if current == "" || current == "dev" || os.Getenv("CI") != "" || os.Getenv("TFREVIEW_NO_UPDATE_CHECK") != "" {
		return "", false
	}

	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return "", false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", false
	}
	var release struct {
		TagName    string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil || release.Draft || release.Prerelease {
		return "", false
	}
	if compareVersions(release.TagName, current) <= 0 {
		return "", false
	}
	return release.TagName, true
}

// compareVersions compares the numeric components of release tags such as
// v1.2.3. Invalid or development versions are treated as older than releases.
func compareVersions(a, b string) int {
	parse := func(s string) ([]uint64, bool) {
		s = strings.TrimPrefix(strings.TrimSpace(s), "v")
		if i := strings.IndexAny(s, "+-"); i >= 0 {
			s = s[:i]
		}
		parts := strings.Split(s, ".")
		if len(parts) != 3 {
			return nil, false
		}
		values := make([]uint64, 3)
		for i, part := range parts {
			value, err := strconv.ParseUint(part, 10, 64)
			if err != nil {
				return nil, false
			}
			values[i] = value
		}
		return values, true
	}

	left, leftOK := parse(a)
	right, rightOK := parse(b)
	if !leftOK {
		if rightOK {
			return 1
		}
		return 0
	}
	if !rightOK {
		return -1
	}
	for i := range left {
		if left[i] < right[i] {
			return -1
		}
		if left[i] > right[i] {
			return 1
		}
	}
	return 0
}
