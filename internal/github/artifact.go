package github

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type artifact struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	DownloadURL string `json:"archive_download_url"`
	CreatedAt   string `json:"created_at"`
}

// PR の head に対する成功 run から name の artifact を落として展開する。
// 同名が複数あれば created_at が最新のもの。
func (c *Client) FetchPlanArtifact(ctx context.Context, pr int, name, outDir string) error {
	var pull struct {
		Head struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	if err := c.do(ctx, "GET", fmt.Sprintf("/repos/%s/pulls/%d", c.Repo, pr), nil, &pull); err != nil {
		return err
	}
	var runs struct {
		WorkflowRuns []struct {
			ID int64 `json:"id"`
		} `json:"workflow_runs"`
	}
	if err := c.do(ctx, "GET", fmt.Sprintf("/repos/%s/actions/runs?head_sha=%s&status=success&per_page=50", c.Repo, pull.Head.SHA), nil, &runs); err != nil {
		return err
	}
	var best *artifact
	for _, run := range runs.WorkflowRuns {
		var list struct {
			Artifacts []artifact `json:"artifacts"`
		}
		if err := c.do(ctx, "GET", fmt.Sprintf("/repos/%s/actions/runs/%d/artifacts", c.Repo, run.ID), nil, &list); err != nil {
			return err
		}
		for i := range list.Artifacts {
			a := list.Artifacts[i]
			if a.Name == name && (best == nil || a.CreatedAt > best.CreatedAt) {
				best = &a
			}
		}
	}
	if best == nil {
		return fmt.Errorf("%w: %q for PR #%d (head %s)", ErrArtifactNotFound, name, pr, pull.Head.SHA)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", best.DownloadURL, nil)
	if err != nil {
		return err
	}
	// api.github.com requires the token on this first request, but archive_download_url
	// 302s to a signed, time-limited blob URL on a different host; the stdlib client
	// strips Authorization on a cross-host redirect, which is exactly what we want —
	// the token must not leak to that third-party storage host.
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("download artifact: %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return unzip(raw, outDir)
}

func unzip(raw []byte, outDir string) error {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || strings.Contains(f.Name, "..") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(outDir, filepath.Base(f.Name)), b, 0o644); err != nil {
			return err
		}
	}
	return nil
}
