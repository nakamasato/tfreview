package render

type strings struct {
	Risk, Incomplete, Category, Hits, Checks, Check, Level, Verdict, Reason string
	Targets, Target, Judgement, Reused, Rejudged, JudgedAt, For, Criteria   string
	NoPlans, NoChanges, Calls, Tokens                                       string
}

var texts = map[string]strings{
	"en": {
		Risk: "Risk", Incomplete: "incomplete", Category: "Category", Hits: "Hits", Checks: "Checks",
		Check: "Check", Level: "Level", Verdict: "Verdict", Reason: "Reason",
		Targets: "Targets", Target: "Target", Judgement: "Judgement", Reused: "reused", Rejudged: "re-judged",
		JudgedAt: "Judged at", For: "for", Criteria: "Criteria",
		NoPlans: "No plan was provided; nothing was evaluated.", NoChanges: "No changes in any target.",
		Calls: "calls", Tokens: "tokens",
	},
	"ja": {
		Risk: "危険度", Incomplete: "判定不完全", Category: "観点", Hits: "該当", Checks: "チェック",
		Check: "チェック", Level: "レベル", Verdict: "判定", Reason: "根拠",
		Targets: "対象", Target: "対象", Judgement: "判定", Reused: "再利用", Rejudged: "再判定",
		JudgedAt: "判定時刻", For: "対象 commit", Criteria: "観点ファイル",
		NoPlans: "plan が渡されていないため、何も評価していません。", NoChanges: "どの対象にも差分がありません。",
		Calls: "回", Tokens: "tokens",
	},
}

func t(lang string) strings {
	if s, ok := texts[lang]; ok {
		return s
	}
	return texts["en"]
}
