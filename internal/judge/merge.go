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

// Merge は同じチェックを複数 target が判定した結果から危険な方を残す。
// 負けた側が「検証不能」「評価できず」だった事実は勝者の reason に残す。
// 黙って捨てると、その情報がレビュアーに一切届かない。
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
