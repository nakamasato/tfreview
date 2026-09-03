package github

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
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

func TestListArtifacts(t *testing.T) {
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
			_, _ = w.Write([]byte(`{"artifacts":[{"id":110,"name":"other","archive_download_url":"` + c.BaseURL + `/dl/110","created_at":"2026-09-02T00:00:00Z","size_in_bytes":42,"expired":true},{"id":111,"name":"tfreview-plan","archive_download_url":"` + c.BaseURL + `/dl/111","created_at":"2026-09-02T01:00:00Z"}]}`))
		default:
			w.WriteHeader(500)
		}
	})
	artifacts, headSHA, err := c.ListArtifacts(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, "abc", headSHA)
	require.Len(t, artifacts, 3)
	// newest first
	require.Equal(t, "tfreview-plan", artifacts[0].Name)
	require.Equal(t, int64(111), artifacts[0].ID)
	require.True(t, artifacts[1].Expired)
	require.Equal(t, int64(42), artifacts[1].SizeInBytes)
}

func TestDownloadArtifact(t *testing.T) {
	archive := zipWith(t, map[string]string{"prd.json": `{"target":"prd"}`})
	var c *Client
	c, _ = server(t, func(w http.ResponseWriter, r *http.Request, _ *[]call) {
		switch r.URL.Path {
		case "/dl/1":
			_, _ = w.Write(archive)
		default:
			w.WriteHeader(500)
		}
	})
	b, err := c.DownloadArtifact(context.Background(), Artifact{ID: 1, DownloadURL: c.BaseURL + "/dl/1"})
	require.NoError(t, err)
	require.Equal(t, archive, b)
}

// TestDownloadArtifactFollowsRedirectWithoutAuth verifies that the PR token
// used against api.github.com does not leak to the blob store that
// archive_download_url redirects to. The stdlib http.Client only strips
// Authorization on redirect when the hostname changes (127.0.0.1 vs
// "localhost" count as different hosts even though both resolve to the same
// loopback address), so the blob server here is addressed as "localhost"
// while the API stub is addressed as "127.0.0.1" to exercise that path.
func TestDownloadArtifactFollowsRedirectWithoutAuth(t *testing.T) {
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
		case "/dl/100":
			http.Redirect(w, r, blobURL, http.StatusFound)
		default:
			w.WriteHeader(500)
		}
	})
	require.True(t, strings.HasPrefix(c.BaseURL, "http://127.0.0.1:"), "test requires the API stub on 127.0.0.1, got %s", c.BaseURL)

	b, err := c.DownloadArtifact(context.Background(), Artifact{ID: 100, DownloadURL: c.BaseURL + "/dl/100"})
	require.NoError(t, err)
	require.Equal(t, archive, b)
	require.Empty(t, blobSawAuth, "Authorization must not be forwarded to the cross-host blob redirect")
}

func TestListArtifactsEmpty(t *testing.T) {
	c, _ := server(t, func(w http.ResponseWriter, r *http.Request, _ *[]call) {
		switch r.URL.Path {
		case "/repos/o/r/pulls/7":
			_, _ = w.Write([]byte(`{"head":{"sha":"abc"}}`))
		case "/repos/o/r/actions/runs":
			_, _ = w.Write([]byte(`{"workflow_runs":[]}`))
		}
	})
	artifacts, headSHA, err := c.ListArtifacts(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, "abc", headSHA)
	require.Empty(t, artifacts)
}
