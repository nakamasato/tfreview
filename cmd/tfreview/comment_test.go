package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nakamasato/tfreview/internal/github"
	"github.com/nakamasato/tfreview/internal/render"
	"github.com/stretchr/testify/require"
)

func stubGitHub(t *testing.T, handler http.HandlerFunc) *[]string {
	t.Helper()
	paths := &[]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*paths = append(*paths, r.Method+" "+r.URL.Path)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	orig := newGitHubClient
	newGitHubClient = func(repo, token string) *github.Client {
		c := github.New(repo, token)
		c.BaseURL = srv.URL
		return c
	}
	t.Cleanup(func() { newGitHubClient = orig })
	t.Setenv("GITHUB_TOKEN", "tok")
	return paths
}

func writeResult(t *testing.T) string {
	t.Helper()
	r := &render.Result{Score: "high", Label: "tfreview:high", Language: "en", HeadSHA: "abc", JudgedAt: "2026-09-02T00:00:00Z"}
	p := filepath.Join(t.TempDir(), "result.json")
	require.NoError(t, r.Save(p))
	return p
}

func TestCommentPostsAndLabels(t *testing.T) {
	paths := stubGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			_, _ = w.Write([]byte(`[]`))
		default:
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{}`))
		}
	})
	require.NoError(t, run(t, "comment", "--result", writeResult(t), "--pr", "7", "--repo", "o/r"))
	require.Contains(t, *paths, "POST /repos/o/r/issues/7/comments")
	require.Contains(t, *paths, "POST /repos/o/r/issues/7/labels")
}

func TestCommentNoLabel(t *testing.T) {
	paths := stubGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{}`))
	})
	require.NoError(t, run(t, "comment", "--result", writeResult(t), "--pr", "7", "--repo", "o/r", "--no-label"))
	require.NotContains(t, *paths, "GET /repos/o/r/issues/7/labels")
}

func TestCommentLabelForbiddenIsWarning(t *testing.T) {
	stubGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET":
			_, _ = w.Write([]byte(`[]`))
		case r.URL.Path == "/repos/o/r/issues/7/labels":
			w.WriteHeader(404)
		case r.URL.Path == "/repos/o/r/labels":
			w.WriteHeader(403)
		default:
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{}`))
		}
	})
	require.NoError(t, run(t, "comment", "--result", writeResult(t), "--pr", "7", "--repo", "o/r"))
}

func TestCommentRepoFromEnv(t *testing.T) {
	paths := stubGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{}`))
	})
	t.Setenv("GITHUB_REPOSITORY", "env/repo")
	resultPath, err := filepath.Abs(writeResult(t))
	require.NoError(t, err)

	// GITHUB_REPOSITORY より下位の git remote を用意し、env が優先されることを確認する。
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"remote", "add", "origin", "git@github.com:other/repo.git"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "git %v: %s", args, out)
	}
	t.Chdir(dir)

	require.NoError(t, run(t, "comment", "--result", resultPath, "--pr", "7", "--no-label"))
	require.Contains(t, *paths, "POST /repos/env/repo/issues/7/comments")
}

func TestCommentRepoFromGitRemote(t *testing.T) {
	paths := stubGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{}`))
	})
	resultPath, err := filepath.Abs(writeResult(t))
	require.NoError(t, err)
	t.Setenv("GITHUB_REPOSITORY", "")

	dir := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"remote", "add", "origin", "git@github.com:env/fromgit.git"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "git %v: %s", args, out)
	}
	t.Chdir(dir)

	require.NoError(t, run(t, "comment", "--result", resultPath, "--pr", "7", "--no-label"))
	require.Contains(t, *paths, "POST /repos/env/fromgit/issues/7/comments")
}

func TestCommentWithoutTokenExit2(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("PATH", t.TempDir()) // gh が見つからないようにする
	err := run(t, "comment", "--result", writeResult(t), "--pr", "7", "--repo", "o/r")
	require.Equal(t, 2, exitCode(err))
}

func TestFetchNotFoundExit1(t *testing.T) {
	stubGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/pulls/7":
			_, _ = w.Write([]byte(`{"head":{"sha":"abc"}}`))
		default:
			_, _ = w.Write([]byte(`{"workflow_runs":[]}`))
		}
	})
	err := run(t, "fetch", "--pr", "7", "--repo", "o/r", "--out-dir", t.TempDir())
	require.Equal(t, 1, exitCode(err))
	require.Contains(t, err.Error(), "no plan artifact")
}

func TestFetchWritesFiles(t *testing.T) {
	archive := zipBytes(t, map[string]string{"prd.json": `{"target":"prd"}`})
	var base string
	stubGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if base == "" {
			base = "http://" + r.Host
		}
		switch r.URL.Path {
		case "/repos/o/r/pulls/7":
			_, _ = w.Write([]byte(`{"head":{"sha":"abc"}}`))
		case "/repos/o/r/actions/runs":
			_, _ = w.Write([]byte(`{"workflow_runs":[{"id":1}]}`))
		case "/repos/o/r/actions/runs/1/artifacts":
			_, _ = w.Write([]byte(`{"artifacts":[{"id":9,"name":"tfreview-plan","archive_download_url":"` + base + `/dl","created_at":"x"}]}`))
		case "/dl":
			_, _ = w.Write(archive)
		}
	})
	dir := t.TempDir()
	require.NoError(t, run(t, "fetch", "--pr", "7", "--repo", "o/r", "--out-dir", dir))
	b, err := os.ReadFile(filepath.Join(dir, "prd.json"))
	require.NoError(t, err)
	require.Contains(t, string(b), "prd")
}

func TestFetchAutoDetectsRawShowJSON(t *testing.T) {
	rawShowJSON := `{"format_version":"1.2","resource_changes":[{"address":"aws_s3_bucket.logs","type":"aws_s3_bucket","name":"logs","change":{"actions":["create"],"before":null,"after":{"bucket":"logs"}}}]}`
	planArchive := zipBytes(t, map[string]string{"tfplan.json": rawShowJSON})
	fileArchive := zipBytes(t, map[string]string{"plan.tfplan": "not json"})

	var base string
	stubGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if base == "" {
			base = "http://" + r.Host
		}
		switch r.URL.Path {
		case "/repos/o/r/pulls/7":
			_, _ = w.Write([]byte(`{"head":{"sha":"abc"}}`))
		case "/repos/o/r/actions/runs":
			_, _ = w.Write([]byte(`{"workflow_runs":[{"id":1}]}`))
		case "/repos/o/r/actions/runs/1/artifacts":
			_, _ = w.Write([]byte(`{"artifacts":[
				{"id":9,"name":"terraform_plan_json_gcp-x","archive_download_url":"` + base + `/dl/plan","created_at":"2026-09-01T00:00:00Z"},
				{"id":10,"name":"terraform_plan_file_gcp-x","archive_download_url":"` + base + `/dl/file","created_at":"2026-09-01T00:00:00Z"}
			]}`))
		case "/dl/plan":
			_, _ = w.Write(planArchive)
		case "/dl/file":
			_, _ = w.Write(fileArchive)
		}
	})
	dir := t.TempDir()
	require.NoError(t, run(t, "fetch", "--pr", "7", "--repo", "o/r", "--out-dir", dir))

	b, err := os.ReadFile(filepath.Join(dir, "gcp-x.json"))
	require.NoError(t, err)
	var p struct {
		Target    string `json:"target"`
		Resources []any  `json:"resources"`
	}
	require.NoError(t, json.Unmarshal(b, &p))
	require.Equal(t, "gcp-x", p.Target)
	require.Len(t, p.Resources, 1)
}

func TestFetchNoUsablePlanArtifactExit1(t *testing.T) {
	var base string
	stubGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if base == "" {
			base = "http://" + r.Host
		}
		switch r.URL.Path {
		case "/repos/o/r/pulls/7":
			_, _ = w.Write([]byte(`{"head":{"sha":"abc"}}`))
		case "/repos/o/r/actions/runs":
			_, _ = w.Write([]byte(`{"workflow_runs":[{"id":1}]}`))
		case "/repos/o/r/actions/runs/1/artifacts":
			_, _ = w.Write([]byte(`{"artifacts":[{"id":9,"name":"build-logs","archive_download_url":"` + base + `/dl","created_at":"2026-09-01T00:00:00Z"}]}`))
		case "/dl":
			_, _ = w.Write(zipBytes(t, map[string]string{"notes.txt": "hello"}))
		}
	})
	err := run(t, "fetch", "--pr", "7", "--repo", "o/r", "--out-dir", t.TempDir())
	require.Equal(t, 1, exitCode(err))
	require.Contains(t, err.Error(), "no plan artifact")
	require.Contains(t, err.Error(), "build-logs")
}

func zipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, _ = w.Write([]byte(body))
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}
