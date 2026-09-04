package main

import (
	"archive/zip"
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const rawShowJSONFixture = `{"format_version":"1.2","resource_changes":[{"address":"aws_s3_bucket.logs","type":"aws_s3_bucket","name":"logs","change":{"actions":["create"],"before":null,"after":{"bucket":"logs"}}}]}`

// testServerURL returns the base URL of the stub server handling r, derived
// from the request itself so tests don't need the httptest.Server value.
func testServerURL(r *http.Request) string {
	return "http://" + r.Host
}

func zipArchive(t *testing.T, files map[string]string) []byte {
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

// TestFetchSkipsExpiredTfreviewPlanArtifact verifies that an expired
// tfreview-plan artifact is skipped (never downloaded) instead of failing the
// whole fetch, falling back to the generic loop to find a usable artifact.
func TestFetchSkipsExpiredTfreviewPlanArtifact(t *testing.T) {
	rawArchive := zipArchive(t, map[string]string{"plan.json": rawShowJSONFixture})
	stubGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/pulls/7":
			_, _ = w.Write([]byte(`{"head":{"sha":"abc"}}`))
		case "/repos/o/r/actions/runs":
			_, _ = w.Write([]byte(`{"workflow_runs":[{"id":10}]}`))
		case "/repos/o/r/actions/runs/10/artifacts":
			_, _ = w.Write([]byte(`{"artifacts":[
				{"id":1,"name":"tfreview-plan","archive_download_url":"` + testServerURL(r) + `/dl/1","created_at":"2026-09-02T01:00:00Z","size_in_bytes":10,"expired":true},
				{"id":2,"name":"plan_dev","archive_download_url":"` + testServerURL(r) + `/dl/2","created_at":"2026-09-02T00:00:00Z","size_in_bytes":10,"expired":false}
			]}`))
		case "/dl/1":
			// The expired artifact must never be downloaded.
			w.WriteHeader(500)
		case "/dl/2":
			_, _ = w.Write(rawArchive)
		default:
			w.WriteHeader(500)
		}
	})

	outDir := t.TempDir()
	require.NoError(t, run(t, "fetch", "--pr", "7", "--repo", "o/r", "--out-dir", outDir))

	b, err := os.ReadFile(filepath.Join(outDir, "dev.json"))
	require.NoError(t, err)
	require.Contains(t, string(b), `"aws_s3_bucket.logs"`)
}

// TestFetchSkipsUnsafeTargetName verifies that a target name coming from
// inside an artifact zip (an already-reduced plan's "target" field, which the
// uploader fully controls) can't be used to write outside outDir.
func TestFetchSkipsUnsafeTargetName(t *testing.T) {
	maliciousArchive := zipArchive(t, map[string]string{
		"plan.json": `{"target":"../evil","counts":{"add":0},"resources":[]}`,
	})
	stubGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/pulls/7":
			_, _ = w.Write([]byte(`{"head":{"sha":"abc"}}`))
		case "/repos/o/r/actions/runs":
			_, _ = w.Write([]byte(`{"workflow_runs":[{"id":10}]}`))
		case "/repos/o/r/actions/runs/10/artifacts":
			_, _ = w.Write([]byte(`{"artifacts":[
				{"id":1,"name":"myplan","archive_download_url":"` + testServerURL(r) + `/dl/1","created_at":"2026-09-02T00:00:00Z","size_in_bytes":10,"expired":false}
			]}`))
		case "/dl/1":
			_, _ = w.Write(maliciousArchive)
		default:
			w.WriteHeader(500)
		}
	})

	outDir := t.TempDir()
	stdout, stderr, err := runCapture(t, "fetch", "--pr", "7", "--repo", "o/r", "--out-dir", outDir, "--artifact", "myplan")
	require.Error(t, err, "no target was safe to write, so fetch should report failure rather than silent success")
	require.Contains(t, stderr, "unsafe target")

	// Nothing must have escaped outDir, and outDir itself must stay empty.
	_, statErr := os.Stat(filepath.Join(filepath.Dir(outDir), "evil.json"))
	require.True(t, os.IsNotExist(statErr))
	entries, _ := os.ReadDir(outDir)
	require.Empty(t, entries)
	_ = stdout
}

func TestIsValidTargetName(t *testing.T) {
	require.True(t, isValidTargetName("dev"))
	require.True(t, isValidTargetName("gcp-x"))
	require.False(t, isValidTargetName(""))
	require.False(t, isValidTargetName("../evil"))
	require.False(t, isValidTargetName("a/b"))
	require.False(t, isValidTargetName(`a\b`))
	require.False(t, isValidTargetName("a..b"))
}
