// Package planfind locates and classifies plan JSON inside a CI artifact zip,
// so that `fetch` can work with pipelines that upload raw `terraform show
// -json` output instead of a pre-reduced tfreview plan.
package planfind

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nakamasato/tfreview/internal/plan"
)

type Kind int

const (
	KindOther Kind = iota
	KindRaw
	KindReduced
)

// maxUncompressedBytes bounds how many decompressed bytes readZipFiles will
// produce from a single artifact zip (per file and in total), defending
// against zip bombs. The compressed-size check the caller does against the
// artifact metadata only bounds the size on disk, not what a small archive
// can expand to.
const maxUncompressedBytes = 200 * 1024 * 1024

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

	// Iterate in a deterministic order (map iteration order is random in
	// Go), so that which entry "wins" a target name collision doesn't change
	// from run to run.
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	type entry struct {
		name string
		kind Kind
		b    []byte
	}
	var entries []entry
	rawCount := 0
	for _, name := range names {
		if !strings.HasSuffix(strings.ToLower(name), ".json") {
			continue
		}
		b := files[name]
		kind := Classify(b)
		if kind == KindRaw {
			rawCount++
		}
		entries = append(entries, entry{name: name, kind: kind, b: b})
	}

	baseTarget := TargetFromArtifact(artifactName, extraPrefixes)
	var found []Found
	for _, e := range entries {
		switch e.kind {
		case KindRaw:
			target := baseTarget
			if rawCount > 1 {
				// The artifact name alone can't tell multiple raw show-json
				// files in the same artifact apart (they'd all derive the
				// same target and silently collide), so fold in something
				// derived from the entry's own path within the zip.
				target = baseTarget + "-" + rawEntrySuffix(e.name)
			}
			p, err := plan.Extract(e.b, target)
			if err != nil {
				// Looked like raw show-json (format_version + resource_changes)
				// but didn't actually parse as one; skip rather than fail the
				// whole fetch over one bad file.
				continue
			}
			found = append(found, Found{Target: target, Kind: KindRaw, Plan: p})
		case KindReduced:
			var p plan.Plan
			if err := json.Unmarshal(e.b, &p); err != nil {
				continue
			}
			found = append(found, Found{Target: p.Target, Kind: KindReduced, Plan: &p})
		}
	}
	return found, nil
}

// rawEntrySuffix derives a short, filesystem-safe suffix from a zip entry's
// path, used to tell apart multiple raw show-json plans found within the
// same artifact.
func rawEntrySuffix(entryPath string) string {
	dir := path.Dir(entryPath)
	base := strings.TrimSuffix(path.Base(entryPath), path.Ext(entryPath))
	suffix := base
	if dir != "." && dir != "/" {
		suffix = strings.Trim(dir, "/")
		suffix = strings.ReplaceAll(suffix, "/", "-") + "-" + base
	}
	suffix = strings.ReplaceAll(suffix, string(filepath.Separator), "-")
	suffix = strings.ReplaceAll(suffix, "..", "-")
	suffix = strings.Trim(suffix, "-")
	if suffix == "" {
		suffix = "plan"
	}
	return suffix
}

// ExtractAll writes every file in a CI artifact zip into outDir, preserving
// the entry's path within the zip. Used for the `tfreview-plan` artifact,
// whose contents are already reduced plans (conventionally flat
// `<target>.json` files, but nothing stops a producer from nesting them).
func ExtractAll(zipBytes []byte, outDir string) error {
	files, err := readZipFiles(zipBytes)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	for name, b := range files {
		dest := filepath.Join(outDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dest, b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// readZipFiles extracts every regular file from a zip archive, keyed by its
// full path within the archive (not just its base name), so that entries
// with the same file name in different directories don't clobber each other.
// Entries that look like a zip-slip attempt are skipped, and both per-file
// and total decompressed size are capped to guard against zip bombs.
func readZipFiles(zipBytes []byte) (map[string][]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	var total int64
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !isSafeZipEntryName(f.Name) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(rc, maxUncompressedBytes+1))
		_ = rc.Close()
		if err != nil {
			return nil, err
		}
		if int64(len(b)) > maxUncompressedBytes {
			return nil, fmt.Errorf("planfind: entry %q exceeds max uncompressed size of %d bytes", f.Name, maxUncompressedBytes)
		}
		total += int64(len(b))
		if total > maxUncompressedBytes {
			return nil, fmt.Errorf("planfind: archive exceeds max total uncompressed size of %d bytes", maxUncompressedBytes)
		}
		out[f.Name] = b
	}
	return out, nil
}

// isSafeZipEntryName reports whether a zip entry's path is safe to extract:
// relative, and never escaping the extraction root via "..", however it's
// spelled. A plain strings.Contains(name, "..") check (the previous approach)
// is too weak: it doesn't catch an absolute path, and cleaning the path can
// reveal a ".." that wasn't contiguous in the raw entry name.
func isSafeZipEntryName(name string) bool {
	if name == "" {
		return false
	}
	// Zip entry names always use "/", regardless of OS; check each raw
	// segment before cleaning, since Clean can collapse a segment like
	// "a/../.." down to something that looks safe once merged.
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return false
		}
	}
	cleaned := path.Clean(name)
	if path.IsAbs(cleaned) {
		return false
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return false
	}
	return true
}
