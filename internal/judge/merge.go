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
	winnerIdx := 0
	for i := 1; i < len(vs); i++ {
		if vs[i].Kind.Rank() > vs[winnerIdx].Kind.Rank() {
			winnerIdx = i
		}
	}
	winner := vs[winnerIdx]
	notes := map[string]bool{}
	for i, v := range vs {
		// Skip the winner itself, and skip any loser whose kind matches the winner's:
		// the winner's own Kind already tells the reviewer that, so a note repeating
		// it (e.g. "(other targets: not evaluated)" on a Skipped winner) is noise.
		if i == winnerIdx || v.Kind == winner.Kind {
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
