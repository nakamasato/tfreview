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

// maxListPages caps the number of pages fetched for a single paginated
// listing (workflow runs, or artifacts within one run). It guards against an
// endless loop if the API ever kept reporting a full page; at 100 items per
// page that's still tens of thousands of items, far more than any real PR
// produces.
const maxListPages = 100

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
	const runsPerPage = 50
	var runIDs []int64
	for page := 1; page <= maxListPages; page++ {
		var runs struct {
			WorkflowRuns []struct {
				ID int64 `json:"id"`
			} `json:"workflow_runs"`
		}
		path := fmt.Sprintf("/repos/%s/actions/runs?head_sha=%s&per_page=%d&page=%d", c.Repo, pull.Head.SHA, runsPerPage, page)
		if err := c.do(ctx, "GET", path, nil, &runs); err != nil {
			return nil, "", err
		}
		for _, run := range runs.WorkflowRuns {
			runIDs = append(runIDs, run.ID)
		}
		if len(runs.WorkflowRuns) < runsPerPage {
			break
		}
	}

	const artifactsPerPage = 100
	var all []Artifact
	for _, runID := range runIDs {
		for page := 1; page <= maxListPages; page++ {
			var list struct {
				Artifacts []artifactJSON `json:"artifacts"`
			}
			path := fmt.Sprintf("/repos/%s/actions/runs/%d/artifacts?per_page=%d&page=%d", c.Repo, runID, artifactsPerPage, page)
			if err := c.do(ctx, "GET", path, nil, &list); err != nil {
				return nil, "", err
			}
			for _, a := range list.Artifacts {
				all = append(all, Artifact(a))
			}
			if len(list.Artifacts) < artifactsPerPage {
				break
			}
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
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download artifact: %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
