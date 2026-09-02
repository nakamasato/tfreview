package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
)

// Artifact is one GitHub Actions run artifact.
type Artifact struct {
	ID          int64
	Name        string
	DownloadURL string
	CreatedAt   string
	SizeInBytes int64
	Expired     bool
}

type artifactJSON struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	DownloadURL string `json:"archive_download_url"`
	CreatedAt   string `json:"created_at"`
	SizeInBytes int64  `json:"size_in_bytes"`
	Expired     bool   `json:"expired"`
}

// ListArtifacts lists every artifact across the workflow runs at the PR's
// head SHA (successful or not: the action uploads artifacts before a
// fail-on check would abort the run), newest first, along with the head SHA.
func (c *Client) ListArtifacts(ctx context.Context, pr int) ([]Artifact, string, error) {
	var pull struct {
		Head struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	if err := c.do(ctx, "GET", fmt.Sprintf("/repos/%s/pulls/%d", c.Repo, pr), nil, &pull); err != nil {
		return nil, "", err
	}
	var runs struct {
		WorkflowRuns []struct {
			ID int64 `json:"id"`
		} `json:"workflow_runs"`
	}
	if err := c.do(ctx, "GET", fmt.Sprintf("/repos/%s/actions/runs?head_sha=%s&per_page=50", c.Repo, pull.Head.SHA), nil, &runs); err != nil {
		return nil, "", err
	}
	var all []Artifact
	for _, run := range runs.WorkflowRuns {
		var list struct {
			Artifacts []artifactJSON `json:"artifacts"`
		}
		if err := c.do(ctx, "GET", fmt.Sprintf("/repos/%s/actions/runs/%d/artifacts?per_page=100", c.Repo, run.ID), nil, &list); err != nil {
			return nil, "", err
		}
		for _, a := range list.Artifacts {
			all = append(all, Artifact{
				ID:          a.ID,
				Name:        a.Name,
				DownloadURL: a.DownloadURL,
				CreatedAt:   a.CreatedAt,
				SizeInBytes: a.SizeInBytes,
				Expired:     a.Expired,
			})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].CreatedAt > all[j].CreatedAt })
	return all, pull.Head.SHA, nil
}

// DownloadArtifact fetches an artifact's zip contents.
func (c *Client) DownloadArtifact(ctx context.Context, a Artifact) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", a.DownloadURL, nil)
	if err != nil {
		return nil, err
	}
	// api.github.com requires the token on this first request, but archive_download_url
	// 302s to a signed, time-limited blob URL on a different host; the stdlib client
	// strips Authorization on a cross-host redirect, which is exactly what we want —
	// the token must not leak to that third-party storage host.
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download artifact: %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
