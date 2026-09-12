// Package tfdiff reduces a PR's unified diff to the part that can explain a plan.
//
// A raw diff competes with the plan for the same context budget and in a monorepo
// dwarfs it, so hunks are attributed to their enclosing HCL block and dropped when
// they cannot bear on the plan. lifecycle and scanner-suppression hunks are the
// exception: they are the signals the plan cannot carry at all.
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
	blockRE       = regexp.MustCompile(`^[+\- ]?\s*(resource|module|locals|variable|data|output)\b\s*"?([a-zA-Z0-9_-]*)"?`)
	suppressionRE = regexp.MustCompile(`(?i)(trivy:ignore|tfsec:ignore|checkov:skip|nosec|terraform:ignore)`)
	diffGitRE     = regexp.MustCompile(`^diff --git a/(\S+) b/(\S+)`)
)

var keptExtensions = []string{".tf", ".tfvars", ".hcl"}

type hunk struct {
	file      string
	blockKind string
	blockType string
	lines     []string
	priority  int
}

// Reduce walks a unified diff line by line, attributes each hunk to the HCL block it
// falls in, and keeps only what can explain planTypes plus lifecycle/suppression hunks
// (which the plan can never surface on its own), trimming lowest-priority hunks whole
// until the result fits the budget.
func Reduce(raw []byte, planTypes []string, b Budget) Diff {
	hunks, headers := parseDiff(raw)

	var kept []hunk
	omittedHunks := 0
	omittedFiles := map[string]bool{}
	for _, h := range hunks {
		priority, keep := classify(h, planTypes)
		if !keep {
			omittedHunks++
			omittedFiles[h.file] = true
			continue
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

	return Diff{Text: text, Summary: summary(omittedHunks, len(omittedFiles), trimmed)}
}

// parseDiff splits raw into hunks attributed to their enclosing HCL block, dropping
// every line of a file whose extension is not one tfreview understands, and captures
// the "--- a/" / "+++ b/" header pair for each surviving file.
func parseDiff(raw []byte) ([]hunk, map[string][2]string) {
	lines := strings.Split(string(raw), "\n")

	var allHunks []hunk
	headers := map[string][2]string{}

	var curFile string
	var curKept bool
	var curBlockKind, curBlockType string
	var curHunk *hunk
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
			curKept = hasKeptExtension(curFile)
			curBlockKind, curBlockType = "", ""
			headerLinesSeen = 0
		case !curKept:
			// Every line of a dropped file, including its own "diff --git" line's
			// header and hunks, is skipped without inspection.
			continue
		case headerLinesSeen == 0 && strings.HasPrefix(line, "--- "):
			headers[curFile] = [2]string{line, ""}
			headerLinesSeen = 1
		case headerLinesSeen == 1 && strings.HasPrefix(line, "+++ "):
			h := headers[curFile]
			h[1] = line
			headers[curFile] = h
			headerLinesSeen = 2
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
			}
			curHunk.lines = append(curHunk.lines, line)
		}
	}
	finalizeHunk()

	return allHunks, headers
}

func fileFromDiffGitLine(line string) string {
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

// classify returns the hunk's trim priority (lowest trimmed first) and whether it
// survives at all. lifecycle/suppression hunks outrank everything because the plan
// carries no signal for them; among the rest, resource hunks are the cheapest to lose
// since the plan already lists the resource's actual after-state.
func classify(h hunk, planTypes []string) (priority int, keep bool) {
	if hasLifecycleOrSuppression(h) {
		return 3, true
	}
	switch h.blockKind {
	case "module", "variable", "locals":
		return 2, true
	case "output", "data":
		return 1, true
	case "resource":
		if slices.Contains(planTypes, h.blockType) {
			return 0, true
		}
	}
	return 0, false
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

func summary(omittedHunks, omittedFiles, trimmed int) string {
	var parts []string
	if omittedHunks > 0 {
		parts = append(parts, fmt.Sprintf("omitted %d hunks across %d files for resource types not in this plan", omittedHunks, omittedFiles))
	}
	if trimmed > 0 {
		parts = append(parts, fmt.Sprintf("trimmed %d further hunks to fit the size budget", trimmed))
	}
	if len(parts) == 0 {
		return "the full Terraform diff is shown"
	}
	return strings.Join(parts, "; ")
}
