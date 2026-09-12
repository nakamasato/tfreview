package judge

import (
	"github.com/nakamasato/tfreview/internal/config"
	"github.com/nakamasato/tfreview/internal/model"
)

// counts treats both hit and unverifiable as contributing to severity. If unverifiable
// were dropped to none, any check that plan alone can't confirm would always show green.
func counts(k model.VerdictKind) bool {
	return k == model.VerdictHit || k == model.VerdictUnverifiable
}

func CategoryScore(cat model.Aspect, verdicts map[string]model.Verdict) model.Severity {
	score := model.SeverityNone
	for _, ck := range cat.Checks {
		if v, ok := verdicts[ck.ID]; ok && counts(v.Kind) {
			score = model.MaxSeverity(score, ck.Severity)
		}
	}
	return score
}

// Use max, not sum or average: averaging would dilute a single critical hit
// among nine passing checks.
func Score(cfg *config.Config, verdicts map[string]model.Verdict) model.Severity {
	score := model.SeverityNone
	for _, cat := range cfg.Categories {
		score = model.MaxSeverity(score, CategoryScore(cat, verdicts))
	}
	return score
}

func RuleScore(cfg *config.Config, verdicts map[string]model.Verdict) model.Severity {
	filtered := map[string]model.Verdict{}
	for id, v := range verdicts {
		if v.Source == model.SourceRule {
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
