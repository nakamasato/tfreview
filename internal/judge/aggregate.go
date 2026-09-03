package judge

import (
	"github.com/nakamasato/tfreview/internal/config"
	"github.com/nakamasato/tfreview/internal/model"
)

// counts は hit と unverifiable を危険度に数える。unverifiable を none に落とすと
// 「plan では見えない」観点が常に緑になる。
func counts(k model.VerdictKind) bool {
	return k == model.VerdictHit || k == model.VerdictUnverifiable
}

func CategoryScore(cat model.Category, verdicts map[string]model.Verdict) model.Level {
	score := model.LevelNone
	for _, ck := range cat.Checks {
		if v, ok := verdicts[ck.ID]; ok && counts(v.Kind) {
			score = model.MaxLevel(score, ck.Level)
		}
	}
	return score
}

// 合計や平均にすると critical 1 件が該当なし 9 件に薄められるので max。
func Score(cfg *config.Config, verdicts map[string]model.Verdict) model.Level {
	score := model.LevelNone
	for _, cat := range cfg.Categories {
		score = model.MaxLevel(score, CategoryScore(cat, verdicts))
	}
	return score
}

func MachineScore(cfg *config.Config, verdicts map[string]model.Verdict) model.Level {
	filtered := map[string]model.Verdict{}
	for id, v := range verdicts {
		if v.Source == model.SourceMachine {
			filtered[id] = v
		}
	}
	return Score(cfg, filtered)
}

func IsIncomplete(verdicts map[string]model.Verdict, unevaluated map[string]bool) bool {
	if len(unevaluated) > 0 {
		return true
	}
	for _, v := range verdicts {
		if v.Kind == model.VerdictSkipped {
			return true
		}
	}
	return false
}
