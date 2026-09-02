package planfind

import (
	"archive/zip"
	"bytes"
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
