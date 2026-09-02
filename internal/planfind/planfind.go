// Package planfind locates and classifies plan JSON inside a CI artifact zip,
// so that `fetch` can work with pipelines that upload raw `terraform show
// -json` output instead of a pre-reduced tfreview plan.
package planfind

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nakamasato/tfreview/internal/plan"
)

type Kind int

const (
	KindOther Kind = iota
	KindRaw
	KindReduced
)

// builtinPrefixes are stripped from an artifact name to derive a target name,
// in order, before falling back to the name as-is. They cover the artifact
// naming conventions seen across common CI pipelines that upload
// `terraform show -json` per directory/environment.
var builtinPrefixes = []string{
	"terraform_plan_json_",
	"terraform-plan-json-",
	"tfplan-json-",
	"tfplan_json_",
	"plan-json-",
	"plan_json_",
	"plan-",
	"plan_",
}

// Classify inspects the top-level shape of a JSON document to tell a raw
// `terraform show -json` plan apart from one already reduced by
// `tfreview extract`, without depending on either package's exact schema.
func Classify(raw []byte) Kind {
	var probe struct {
		FormatVersion   *string          `json:"format_version"`
		ResourceChanges *json.RawMessage `json:"resource_changes"`
		Target          *string          `json:"target"`
		Resources       *json.RawMessage `json:"resources"`
		Counts          *json.RawMessage `json:"counts"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return KindOther
	}
	switch {
	case probe.FormatVersion != nil && probe.ResourceChanges != nil:
		return KindRaw
	case probe.Target != nil && probe.Resources != nil && probe.Counts != nil:
		return KindReduced
	default:
		return KindOther
	}
}

// TargetFromArtifact derives a plan target name from a CI artifact name by
// stripping the first matching prefix (extraPrefixes take priority over the
// built-in ones) and any trailing ".json".
func TargetFromArtifact(name string, extraPrefixes []string) string {
	target := name
	for _, p := range append(append([]string{}, extraPrefixes...), builtinPrefixes...) {
		if strings.HasPrefix(target, p) {
			target = strings.TrimPrefix(target, p)
			break
		}
	}
	return strings.TrimSuffix(target, ".json")
}

// Found is one plan recovered from inside an artifact zip.
type Found struct {
	Target string
	Kind   Kind
	Plan   *plan.Plan
}

// FromZip scans every *.json entry in a CI artifact zip and returns the ones
// that look like a plan (raw show-json, extracted via plan.Extract, or
// already-reduced). Non-JSON entries and anything else are ignored.
func FromZip(zipBytes []byte, artifactName string, extraPrefixes []string) ([]Found, error) {
	files, err := readZipFiles(zipBytes)
	if err != nil {
		return nil, err
	}
	var found []Found
	for name, b := range files {
		if !strings.HasSuffix(strings.ToLower(name), ".json") {
			continue
		}
		switch Classify(b) {
		case KindRaw:
			target := TargetFromArtifact(artifactName, extraPrefixes)
			p, err := plan.Extract(b, target)
			if err != nil {
				// Looked like raw show-json (format_version + resource_changes)
				// but didn't actually parse as one; skip rather than fail the
				// whole fetch over one bad file.
				continue
			}
			found = append(found, Found{Target: target, Kind: KindRaw, Plan: p})
		case KindReduced:
			var p plan.Plan
			if err := json.Unmarshal(b, &p); err != nil {
				continue
			}
			found = append(found, Found{Target: p.Target, Kind: KindReduced, Plan: &p})
		}
	}
	return found, nil
}

// ExtractAll writes every file in a CI artifact zip into outDir under its
// original base name, for the `tfreview-plan` artifact whose contents are
// already reduced plans named `<target>.json` by convention.
func ExtractAll(zipBytes []byte, outDir string) error {
	files, err := readZipFiles(zipBytes)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	for name, b := range files {
		if err := os.WriteFile(filepath.Join(outDir, name), b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// readZipFiles extracts every regular file from a zip archive, keyed by its
// base name. Entries whose path contains ".." are skipped (zip-slip).
func readZipFiles(zipBytes []byte) (map[string][]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || strings.Contains(f.Name, "..") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		out[filepath.Base(f.Name)] = b
	}
	return out, nil
}
