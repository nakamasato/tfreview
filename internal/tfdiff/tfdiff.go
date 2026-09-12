// Package tfdiff reduces a PR's unified diff to the part that can explain a plan.
//
// A raw diff competes with the plan for the same context budget and in a monorepo
// dwarfs it, so hunks are attributed to their enclosing HCL block and dropped when
// they cannot bear on the plan. lifecycle and scanner-suppression hunks are the
// exception: they are the signals the plan cannot carry at all. A hunk is dropped
// only when its enclosing resource type is known for certain and absent from the
// plan; anything the parser cannot pin down with confidence is kept, because a tool
// whose job is explaining plan diffs must never silently withhold the one hunk that
// would have explained one.
package tfdiff

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

type Diff struct {
	Text    string
	Summary string
}

func (d Diff) IsEmpty() bool { return strings.TrimSpace(d.Text) == "" }

type Budget struct {
	MaxDiffChars  int
	MaxInputChars int
	Used          int
}

func (b Budget) allowance() int {
	room := b.MaxInputChars - b.Used
	if room < 0 {
		room = 0
	}
	return min(b.MaxDiffChars, room)
}

var (
	blockRE         = regexp.MustCompile(`^[+\- ]?\s*(resource|module|locals|variable|data|output)\b\s*"?([a-zA-Z0-9_-]*)"?`)
	suppressionRE   = regexp.MustCompile(`(?i)(trivy:ignore|tfsec:ignore|checkov:skip|nosec|terraform:ignore)`)
	diffGitRE       = regexp.MustCompile(`^diff --git a/(\S+) b/(\S+)`)
	diffGitQuotedRE = regexp.MustCompile(`^diff --git "([^"]*)" "([^"]*)"`)
)

var keptExtensions = []string{".tf", ".tfvars", ".hcl"}

type hunk struct {
	file      string
	blockKind string
	blockType string
	// attributed is true only when a resource|module|... header appeared inside this
	// hunk's own lines. When false, blockKind/blockType (if any) were carried over
	// from an earlier hunk in the same file and may belong to a different block that
	// closed in between without ever showing its own header in a hunk's context.
	attributed bool
	lines      []string
	priority   int
}

// Reduce walks a unified diff line by line, attributes each hunk to the HCL block it
// falls in, and keeps everything except hunks confidently attributed to a resource
// type absent from planTypes, trimming lowest-priority hunks whole until the result
// fits the budget.
func Reduce(raw []byte, planTypes []string, b Budget) Diff {
	hunks, headers := parseDiff(raw)

	var kept []hunk
	excludedHunks := 0
	excludedFiles := map[string]bool{}
	uncertainKept := 0
	for _, h := range hunks {
		priority, keep, uncertain := classify(h, planTypes)
		if !keep {
			excludedHunks++
			excludedFiles[h.file] = true
			continue
		}
		if uncertain {
			uncertainKept++
		}
		h.priority = priority
		kept = append(kept, h)
	}

	allowance := b.allowance()
	text := renderHunks(kept, headers)
	trimmed := 0
	for len(text) > allowance && len(kept) > 0 {
		kept = dropLowestPriority(kept)
		trimmed++
		text = renderHunks(kept, headers)
	}

	return Diff{Text: text, Summary: summary(excludedHunks, len(excludedFiles), uncertainKept, trimmed, len(hunks))}
}

// parseDiff splits raw into hunks attributed to their enclosing HCL block, dropping
// every line of a file whose extension is not one tfreview understands, and captures
// the "--- a/" / "+++ b/" header pair for each surviving file. The file path is read
// from the "+++ b/..." line (or, for a deleted file whose "+++" reads "/dev/null",
// from the "--- a/..." line), since git leaves those lines unquoted and unambiguous
// even when the path contains a space; the "diff --git" line only backs up the path
// when neither header line yields one, and is itself quoted when ambiguous.
func parseDiff(raw []byte) ([]hunk, map[string][2]string) {
	lines := strings.Split(string(raw), "\n")

	var allHunks []hunk
	headers := map[string][2]string{}

	var curFile string
	var curKept bool
	var curBlockKind, curBlockType string
	var curHunk *hunk
	var pendingMinus string
	headerLinesSeen := 0

	finalizeHunk := func() {
		if curHunk != nil {
			allHunks = append(allHunks, *curHunk)
			curHunk = nil
		}
	}

	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			finalizeHunk()
			curFile = fileFromDiffGitLine(line)
			curKept = curFile != "" && hasKeptExtension(curFile)
			curBlockKind, curBlockType = "", ""
			pendingMinus = ""
			headerLinesSeen = 0
		case headerLinesSeen == 0 && strings.HasPrefix(line, "--- "):
			pendingMinus = line
			headerLinesSeen = 1
		case headerLinesSeen == 1 && strings.HasPrefix(line, "+++ "):
			headerLinesSeen = 2
			resolved := resolveFile(pendingMinus, line)
			if resolved != "" {
				curFile = resolved
			}
			curKept = curFile != "" && hasKeptExtension(curFile)
			if curKept {
				headers[curFile] = [2]string{pendingMinus, line}
			}
		case !curKept:
			// Every remaining line of a dropped file, including its hunks, is
			// skipped without inspection.
			continue
		case strings.HasPrefix(line, "@@"):
			finalizeHunk()
			curHunk = &hunk{file: curFile, blockKind: curBlockKind, blockType: curBlockType}
			curHunk.lines = append(curHunk.lines, line)
		default:
			if curHunk == nil {
				continue
			}
			if m := blockRE.FindStringSubmatch(line); m != nil {
				curBlockKind, curBlockType = m[1], m[2]
				curHunk.blockKind, curHunk.blockType = curBlockKind, curBlockType
				curHunk.attributed = true
			}
			curHunk.lines = append(curHunk.lines, line)
		}
	}
	finalizeHunk()

	return allHunks, headers
}

// resolveFile determines a file's real path from its "--- a/..." and "+++ b/..."
// header lines. A deleted file's "+++" reads "/dev/null" and carries no path, so the
// "---" line is used instead.
func resolveFile(minusLine, plusLine string) string {
	plusField := strings.TrimPrefix(plusLine, "+++ ")
	if strings.TrimSpace(plusField) == "/dev/null" {
		return extractPath(strings.TrimPrefix(minusLine, "--- "))
	}
	return extractPath(plusField)
}

// extractPath strips a trailing tab (unified diff headers may carry a timestamp
// after one), unquotes a C-style quoted path, and strips a leading "a/" or "b/".
func extractPath(field string) string {
	field = strings.TrimSpace(field)
	if idx := strings.IndexByte(field, '\t'); idx >= 0 {
		field = field[:idx]
	}
	if len(field) >= 2 && strings.HasPrefix(field, `"`) && strings.HasSuffix(field, `"`) {
		field = field[1 : len(field)-1]
	}
	field = strings.TrimPrefix(field, "a/")
	field = strings.TrimPrefix(field, "b/")
	return field
}

// fileFromDiffGitLine is the fallback path source, used only when the header lines
// could not resolve one. git quotes this line's two paths as a whole when either
// contains a character (a space included) that would make "a/X b/Y" ambiguous.
func fileFromDiffGitLine(line string) string {
	if m := diffGitQuotedRE.FindStringSubmatch(line); m != nil {
		return extractPath(m[2])
	}
	if m := diffGitRE.FindStringSubmatch(line); m != nil {
		return m[2]
	}
	return ""
}

func hasKeptExtension(file string) bool {
	for _, ext := range keptExtensions {
		if strings.HasSuffix(file, ext) {
			return true
		}
	}
	return false
}

// classify returns the hunk's trim priority (lowest trimmed first), whether it
// survives at all, and whether it was kept despite an uncertain or absent
// attribution (as opposed to a positive match). lifecycle/suppression hunks outrank
// everything because the plan carries no signal for them. A resource hunk is dropped
// only when its own context lines (not an inherited header from an earlier hunk)
// name a type absent from planTypes: dropping on an inherited or missing attribution
// risks discarding the one hunk that explains the plan, on nothing more than a
// guess.
func classify(h hunk, planTypes []string) (priority int, keep bool, uncertain bool) {
	if hasLifecycleOrSuppression(h) {
		return 3, true, false
	}
	switch h.blockKind {
	case "module", "variable", "locals":
		return 2, true, false
	case "output", "data":
		return 1, true, false
	case "resource":
		if slices.Contains(planTypes, h.blockType) {
			return 0, true, false
		}
		if h.attributed {
			return 0, false, false
		}
		// Inherited attribution to an excluded type: the hunk may actually belong
		// to a different, unheadered block, so keep it rather than guess.
		return 0, true, true
	default:
		// No header has been seen anywhere in this file yet.
		return 0, true, true
	}
}

func hasLifecycleOrSuppression(h hunk) bool {
	for _, l := range h.lines {
		if strings.Contains(l, "lifecycle") || suppressionRE.MatchString(l) {
			return true
		}
	}
	return false
}

// dropLowestPriority removes the last (by original order) hunk among the current
// lowest priority present, so trimming eats whole hunks and never touches a hunk
// ranked higher than one that has already been dropped.
func dropLowestPriority(hs []hunk) []hunk {
	minPriority := hs[0].priority
	for _, h := range hs {
		if h.priority < minPriority {
			minPriority = h.priority
		}
	}
	idx := -1
	for i, h := range hs {
		if h.priority == minPriority {
			idx = i
		}
	}
	return slices.Delete(hs, idx, idx+1)
}

func renderHunks(hs []hunk, headers map[string][2]string) string {
	var sb strings.Builder
	emitted := map[string]bool{}
	for _, h := range hs {
		if !emitted[h.file] {
			if hdr, ok := headers[h.file]; ok {
				sb.WriteString(hdr[0])
				sb.WriteString("\n")
				sb.WriteString(hdr[1])
				sb.WriteString("\n")
			}
			emitted[h.file] = true
		}
		for _, l := range h.lines {
			sb.WriteString(l)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func summary(excludedHunks, excludedFiles, uncertainKept, trimmed, totalHunks int) string {
	var parts []string
	if excludedHunks > 0 {
		parts = append(parts, fmt.Sprintf("omitted %d hunks across %d files for resource types not in this plan", excludedHunks, excludedFiles))
	}
	if uncertainKept > 0 {
		parts = append(parts, fmt.Sprintf("%d hunks whose enclosing block could not be determined were kept", uncertainKept))
	}
	if trimmed > 0 {
		parts = append(parts, fmt.Sprintf("trimmed %d further hunks to fit the size budget", trimmed))
	}
	if len(parts) == 0 {
		if totalHunks == 0 {
			return "no Terraform hunks were found in this diff"
		}
		return "the full Terraform diff is shown"
	}
	return strings.Join(parts, "; ")
}
