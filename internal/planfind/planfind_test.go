package planfind

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const rawShowJSON = `{"format_version":"1.2","resource_changes":[{"address":"aws_s3_bucket.logs","type":"aws_s3_bucket","name":"logs","change":{"actions":["create"],"before":null,"after":{"bucket":"logs"}}}]}`
const reducedJSON = `{"target":"prd","counts":{"add":1},"resources":[{"address":"aws_s3_bucket.logs","type":"aws_s3_bucket","name":"logs","actions":["create"],"after":{"bucket":"logs"}}]}`

func TestClassify(t *testing.T) {
	require.Equal(t, KindRaw, Classify([]byte(rawShowJSON)))
	require.Equal(t, KindReduced, Classify([]byte(reducedJSON)))
	require.Equal(t, KindOther, Classify([]byte(`{"hello":"world"}`)))
	require.Equal(t, KindOther, Classify([]byte(`not json`)))
}

func TestTargetFromArtifact(t *testing.T) {
	require.Equal(t, "gcp-x", TargetFromArtifact("terraform_plan_json_gcp-x", nil))
	require.Equal(t, "prd", TargetFromArtifact("tfplan-json-prd.json", nil))
	require.Equal(t, "x", TargetFromArtifact("custom-prefix-x", []string{"custom-prefix-"}))
	require.Equal(t, "unrelated-artifact", TargetFromArtifact("unrelated-artifact", nil))
	require.Equal(t, "dev", TargetFromArtifact("plan_dev.json", nil))
}

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

func TestFromZip(t *testing.T) {
	archive := zipWith(t, map[string]string{
		"tfplan.json":  rawShowJSON,
		"reduced.json": reducedJSON,
		"notes.json":   `{"hello":"world"}`,
		"plan.bin":     "\x00\x01binary",
		"../evil.json": rawShowJSON,
	})
	found, err := FromZip(archive, "terraform_plan_json_gcp-x", nil)
	require.NoError(t, err)

	var raw, reduced *Found
	for i := range found {
		f := &found[i]
		switch f.Kind {
		case KindRaw:
			raw = f
		case KindReduced:
			reduced = f
		}
	}
	require.NotNil(t, raw, "expected the raw show-json entry to be found")
	require.Equal(t, "gcp-x", raw.Target)
	require.Len(t, raw.Plan.Resources, 1)

	require.NotNil(t, reduced, "expected the reduced entry to be found")
	require.Equal(t, "prd", reduced.Target)
	require.Len(t, reduced.Plan.Resources, 1)

	// notes.json (KindOther) and plan.bin (not .json) must not appear.
	require.Len(t, found, 2)
}

func TestFromZipSkipsPathTraversal(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("../../evil.json")
	require.NoError(t, err)
	_, _ = w.Write([]byte(rawShowJSON))
	require.NoError(t, zw.Close())

	found, err := FromZip(buf.Bytes(), "artifact", nil)
	require.NoError(t, err)
	require.Empty(t, found)
}

// TestFromZipMultipleRawPlansGetDistinctTargets guards against issue #2 T1:
// a single artifact containing more than one raw show-json plan (e.g. one
// per subdirectory) used to make every one of them collide on the same
// target name, derived only from the artifact name, so all but one silently
// disappeared.
func TestFromZipMultipleRawPlansGetDistinctTargets(t *testing.T) {
	archive := zipWith(t, map[string]string{
		"a/plan.json": rawShowJSON,
		"b/plan.json": rawShowJSON,
	})
	found, err := FromZip(archive, "terraform-plan", nil)
	require.NoError(t, err)
	require.Len(t, found, 2)

	targets := map[string]bool{}
	for _, f := range found {
		require.Equal(t, KindRaw, f.Kind)
		require.NotEmpty(t, f.Target)
		targets[f.Target] = true
	}
	require.Len(t, targets, 2, "expected the two raw plans to get distinct target names, got %v", targets)
}

// TestFromZipSameBasenameDifferentDirsBothSurvive guards against the
// non-deterministic map-keyed-by-basename bug: envs/dev/plan.json and
// envs/prod/plan.json must both come back, not have one silently clobber the
// other depending on map iteration order.
func TestFromZipSameBasenameDifferentDirsBothSurvive(t *testing.T) {
	devPlan := `{"target":"dev","counts":{"add":1},"resources":[{"address":"aws_s3_bucket.logs","type":"aws_s3_bucket","name":"logs","actions":["create"],"after":{"bucket":"dev-logs"}}]}`
	prodPlan := `{"target":"prod","counts":{"add":1},"resources":[{"address":"aws_s3_bucket.logs","type":"aws_s3_bucket","name":"logs","actions":["create"],"after":{"bucket":"prod-logs"}}]}`
	archive := zipWith(t, map[string]string{
		"envs/dev/plan.json":  devPlan,
		"envs/prod/plan.json": prodPlan,
	})
	found, err := FromZip(archive, "artifact", nil)
	require.NoError(t, err)
	require.Len(t, found, 2)

	targets := map[string]bool{}
	for _, f := range found {
		require.Equal(t, KindReduced, f.Kind)
		targets[f.Target] = true
	}
	require.True(t, targets["dev"])
	require.True(t, targets["prod"])
}

// TestFromZipRejectsWeirdPathTraversal covers zip-slip spellings that a bare
// strings.Contains(name, "..") check would miss or mishandle.
func TestFromZipRejectsWeirdPathTraversal(t *testing.T) {
	for _, name := range []string{
		"/etc/evil.json",       // absolute path
		"a/../../evil.json",    // escapes root only after cleaning
		"..",                   // bare ".." as the whole name
		"a/..",                 // ".." as the final segment
	} {
		archive := zipWith(t, map[string]string{name: rawShowJSON})
		found, err := FromZip(archive, "artifact", nil)
		require.NoError(t, err, "name=%q", name)
		require.Empty(t, found, "name=%q should have been rejected", name)
	}
}

// TestReadZipFilesRejectsOversizedEntry guards against zip bombs: a small
// compressed file that decompresses far beyond what a plan JSON could ever
// legitimately need must fail instead of allocating unbounded memory.
func TestReadZipFilesRejectsOversizedEntry(t *testing.T) {
	huge := bytes.Repeat([]byte("a"), maxUncompressedBytes+1)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("huge.json")
	require.NoError(t, err)
	_, _ = w.Write(huge)
	require.NoError(t, zw.Close())

	_, err = readZipFiles(buf.Bytes())
	require.Error(t, err)
}

func TestExtractAllPreservesSubdirectories(t *testing.T) {
	archive := zipWith(t, map[string]string{
		"envs/dev/plan.json": reducedJSON,
	})
	dir := t.TempDir()
	require.NoError(t, ExtractAll(archive, dir))

	b, err := os.ReadFile(filepath.Join(dir, "envs", "dev", "plan.json"))
	require.NoError(t, err)
	require.Equal(t, reducedJSON, string(b))
}
