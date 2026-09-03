package judge

import (
	"sort"
	"strings"

	"github.com/nakamasato/tfreview/internal/model"
)

var unresolvedNote = map[model.VerdictKind]string{
	model.VerdictUnverifiable: "unverifiable by plan",
	model.VerdictSkipped:      "not evaluated",
}

// Merge keeps the more severe verdict when multiple targets judge the same check.
// The fact that a losing verdict was "unverifiable" or "not evaluated" is preserved
// in the winner's reason — dropping it silently would hide that information from reviewers.
func Merge(vs []model.Verdict) model.Verdict {
	winner := vs[0]
	for _, v := range vs[1:] {
		if v.Kind.Rank() > winner.Kind.Rank() {
			winner = v
		}
	}
	notes := map[string]bool{}
	for _, v := range vs {
		if v == winner {
			continue
		}
		if n, ok := unresolvedNote[v.Kind]; ok {
			notes[n] = true
		}
	}
	if len(notes) == 0 {
		return winner
	}
	keys := make([]string, 0, len(notes))
	for k := range notes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	winner.Reason = winner.Reason + " (other targets: " + strings.Join(keys, ", ") + ")"
	return winner
}
