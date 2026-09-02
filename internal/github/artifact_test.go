package github

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func zipWith(t *testing.T, files map[string]string) []byte {
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

func TestFetchPlanArtifact(t *testing.T) {
	archive := zipWith(t, map[string]string{"prd.json": `{"target":"prd"}`, "dev.json": `{"target":"dev"}`})
	var c *Client
	c, _ = server(t, func(w http.ResponseWriter, r *http.Request, _ *[]call) {
		switch r.URL.Path {
		case "/repos/o/r/pulls/7":
			_, _ = w.Write([]byte(`{"head":{"sha":"abc"}}`))
		case "/repos/o/r/actions/runs":
			require.Equal(t, "abc", r.URL.Query().Get("head_sha"))
			_, _ = w.Write([]byte(`{"workflow_runs":[{"id":10,"created_at":"2026-09-01T00:00:00Z"},{"id":11,"created_at":"2026-09-02T00:00:00Z"}]}`))
		case "/repos/o/r/actions/runs/10/artifacts":
			_, _ = w.Write([]byte(`{"artifacts":[{"id":100,"name":"tfreview-plan","archive_download_url":"` + c.BaseURL + `/dl/100","created_at":"2026-09-01T00:00:00Z"}]}`))
		case "/repos/o/r/actions/runs/11/artifacts":
			_, _ = w.Write([]byte(`{"artifacts":[{"id":110,"name":"other","archive_download_url":"` + c.BaseURL + `/dl/110","created_at":"2026-09-02T00:00:00Z"},{"id":111,"name":"tfreview-plan","archive_download_url":"` + c.BaseURL + `/dl/111","created_at":"2026-09-02T00:00:00Z"}]}`))
		case "/dl/111":
			_, _ = w.Write(archive)
		default:
			w.WriteHeader(500)
		}
	})
	dir := t.TempDir()
	require.NoError(t, c.FetchPlanArtifact(context.Background(), 7, "tfreview-plan", dir))
	require.FileExists(t, filepath.Join(dir, "prd.json"))
	require.FileExists(t, filepath.Join(dir, "dev.json"))
}

// TestFetchPlanArtifactFollowsRedirectWithoutAuth verifies that the PR token
// used against api.github.com does not leak to the blob store that
// archive_download_url redirects to. The stdlib http.Client only strips
// Authorization on redirect when the hostname changes (127.0.0.1 vs
// "localhost" count as different hosts even though both resolve to the same
// loopback address), so the blob server here is addressed as "localhost"
// while the API stub is addressed as "127.0.0.1" to exercise that path.
func TestFetchPlanArtifactFollowsRedirectWithoutAuth(t *testing.T) {
	archive := zipWith(t, map[string]string{"prd.json": `{"target":"prd"}`})

	blobSawAuth := "unset"
	blob := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		blobSawAuth = r.Header.Get("Authorization")
		_, _ = w.Write(archive)
	}))
	t.Cleanup(blob.Close)
	blobHostPort := strings.TrimPrefix(blob.URL, "http://")
	blobURL := "http://localhost:" + strings.SplitN(blobHostPort, ":", 2)[1] + "/blob"

	var c *Client
	c, _ = server(t, func(w http.ResponseWriter, r *http.Request, _ *[]call) {
		switch r.URL.Path {
		case "/repos/o/r/pulls/7":
			_, _ = w.Write([]byte(`{"head":{"sha":"abc"}}`))
		case "/repos/o/r/actions/runs":
			_, _ = w.Write([]byte(`{"workflow_runs":[{"id":10,"created_at":"2026-09-01T00:00:00Z"}]}`))
		case "/repos/o/r/actions/runs/10/artifacts":
			_, _ = w.Write([]byte(`{"artifacts":[{"id":100,"name":"tfreview-plan","archive_download_url":"` + c.BaseURL + `/dl/100","created_at":"2026-09-01T00:00:00Z"}]}`))
		case "/dl/100":
			http.Redirect(w, r, blobURL, http.StatusFound)
		default:
			w.WriteHeader(500)
		}
	})
	require.True(t, strings.HasPrefix(c.BaseURL, "http://127.0.0.1:"), "test requires the API stub on 127.0.0.1, got %s", c.BaseURL)

	dir := t.TempDir()
	require.NoError(t, c.FetchPlanArtifact(context.Background(), 7, "tfreview-plan", dir))
	require.FileExists(t, filepath.Join(dir, "prd.json"))
	require.Empty(t, blobSawAuth, "Authorization must not be forwarded to the cross-host blob redirect")
}

func TestFetchPlanArtifactNotFound(t *testing.T) {
	c, _ := server(t, func(w http.ResponseWriter, r *http.Request, _ *[]call) {
		switch r.URL.Path {
		case "/repos/o/r/pulls/7":
			_, _ = w.Write([]byte(`{"head":{"sha":"abc"}}`))
		case "/repos/o/r/actions/runs":
			_, _ = w.Write([]byte(`{"workflow_runs":[]}`))
		}
	})
	err := c.FetchPlanArtifact(context.Background(), 7, "tfreview-plan", t.TempDir())
	require.ErrorIs(t, err, ErrArtifactNotFound)
}
