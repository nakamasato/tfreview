package render

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/nakamasato/tfreview/internal/model"
)

const (
	Begin = "<!-- tfreview:begin -->"
	End   = "<!-- tfreview:end -->"
)

var levelEmoji = map[model.Level]string{model.LevelNone: "🟢", model.LevelMedium: "🟡", model.LevelHigh: "🟠", model.LevelCritical: "🔴"}

func Comment(r *Result) string {
	s := t(r.Language)
	var b strings.Builder
	b.WriteString(Begin + "\n")

	// 見出しはテキストだけで危険度が読めるようにする。バッジ（shields.io）は外部
	// サービスなので、落ちたときに情報が消えないよう見出しの重複として置く。
	switch {
	case r.Incomplete:
		if len(r.Unevaluated) > 0 {
			fmt.Fprintf(&b, "## 🔵 %s: %s (%s)\n\n", s.Risk, s.Incomplete, strings.Join(r.Unevaluated, ", "))
		} else {
			fmt.Fprintf(&b, "## 🔵 %s: %s\n\n", s.Risk, s.Incomplete)
		}
	case r.Score == model.LevelNone:
		fmt.Fprintf(&b, "## 🟢 %s: none\n\n", s.Risk)
	default:
		fmt.Fprintf(&b, "## %s %s: %s — %s\n\n", levelEmoji[r.Score], s.Risk, r.Score, topCategory(r))
	}

	if r.NoPlans {
		b.WriteString(s.NoPlans + "\n\n")
		writeMeta(&b, r, s)
		b.WriteString(End + "\n")
		return b.String()
	}

	writeBadges(&b, r)
	writeMeta(&b, r, s)

	if r.NoChanges {
		b.WriteString(s.NoChanges + "\n\n")
		writeTargets(&b, r, s)
		b.WriteString(End + "\n")
		return b.String()
	}

	fmt.Fprintf(&b, "| %s | %s | %s |\n| --- | --- | --- |\n", s.Category, s.Risk, s.Hits)
	for _, c := range r.Categories {
		fmt.Fprintf(&b, "| %s | %s %s | %d/%d |\n", c.Title, levelEmoji[c.Score], c.Score, c.Hits, c.Total)
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "<details><summary>%s</summary>\n\n| %s | %s | %s | %s |\n| --- | --- | --- | --- |\n", s.Checks, s.Check, s.Level, s.Verdict, s.Reason)
	for _, c := range r.Categories {
		for _, ck := range c.Checks {
			icon := "🤖"
			if ck.Source == model.SourceMachine {
				icon = "🔧"
			}
			fmt.Fprintf(&b, "| %s | %s | %s %s | %s |\n", ck.ID, ck.Level, icon, ck.Verdict, cell(ck.Reason))
		}
	}
	b.WriteString("\n</details>\n\n")

	writeTargets(&b, r, s)

	if r.ConfigPath != "" {
		if r.Repo != "" {
			fmt.Fprintf(&b, "%s: [`%s`](https://github.com/%s/blob/%s/%s)\n\n", s.Criteria, r.ConfigPath, r.Repo, r.HeadSHA, r.ConfigPath)
		} else {
			fmt.Fprintf(&b, "%s: `%s`\n\n", s.Criteria, r.ConfigPath)
		}
	}
	if r.Usage.Calls > 0 {
		u := r.Usage
		fmt.Fprintf(&b, "<sub>%s · %d %s · in %s / cache write %s / cache read %s / out %s %s · ≈ $%.4f</sub>\n",
			r.Model, u.Calls, s.Calls, commas(u.InputTokens), commas(u.CacheWriteTokens), commas(u.CacheReadTokens), commas(u.OutputTokens), s.Tokens, r.CostUSD)
	}
	b.WriteString(End + "\n")
	return b.String()
}

func topCategory(r *Result) string {
	for _, c := range r.Categories {
		if c.Score == r.Score {
			return c.Title
		}
	}
	return ""
}

func writeBadges(b *strings.Builder, r *Result) {
	if r.Incomplete {
		// バッジは見出しを反映する。Incomplete のときの見出しは "incomplete" のみなので
		// risk-<score> は出さず、単一の risk-incomplete バッジにする。
		fmt.Fprintf(b, "![incomplete](https://img.shields.io/badge/risk-incomplete-%s)", LabelColor("tfreview:unknown"))
	} else {
		fmt.Fprintf(b, "![%s](https://img.shields.io/badge/risk-%s-%s)", r.Score, badgeText(string(r.Score)), LabelColor("tfreview:"+string(r.Score)))
	}
	for _, c := range r.Categories {
		fmt.Fprintf(b, " ![%s](https://img.shields.io/badge/%s-%d%%2F%d-%s)", c.Title, badgeText(c.Title), c.Hits, c.Total, LabelColor("tfreview:"+string(c.Score)))
	}
	b.WriteString("\n\n")
}

// shields.io は `-` と `_` を区切りに使うので二重にする。残りは URL エスケープ。
func badgeText(s string) string {
	s = strings.ReplaceAll(s, "-", "--")
	s = strings.ReplaceAll(s, "_", "__")
	return url.PathEscape(s)
}

func writeMeta(b *strings.Builder, r *Result, s texts) {
	short := r.HeadSHA
	if len(short) > 7 {
		short = short[:7]
	}
	commit := "`" + short + "`"
	if r.Repo != "" {
		commit = fmt.Sprintf("[`%s`](https://github.com/%s/commit/%s)", short, r.Repo, r.HeadSHA)
	}
	fmt.Fprintf(b, "%s <relative-time datetime=\"%s\">%s</relative-time> %s %s.\n\n", s.JudgedAt, r.JudgedAt, r.JudgedAt, s.For, commit)
}

func writeTargets(b *strings.Builder, r *Result, s texts) {
	fmt.Fprintf(b, "<details><summary>%s</summary>\n\n| %s | + | ~ | - | ± | import | %s |\n| --- | --- | --- | --- | --- | --- | --- |\n", s.Targets, s.Target, s.Judgement)
	for _, t := range r.Targets {
		j := s.Rejudged
		if t.Reused {
			j = s.Reused
		}
		fmt.Fprintf(b, "| %s | %d | %d | %d | %d | %d | %s |\n", t.Target, t.Counts.Add, t.Counts.Change, t.Counts.Destroy, t.Counts.Replace, t.Counts.Import, j)
	}
	b.WriteString("\n</details>\n\n")
}

func cell(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", `\|`)
	return escapeHTMLComments(s)
}

// An LLM-generated reason is free text and might happen to contain the same
// string as a Begin/End marker like "<!-- tfreview:end -->". Left as-is, the next
// UpsertComment/StripBlock call could mistake that part of the comment body for
// the real marker and shift the block boundary. Neutralize the start/end tokens
// so they aren't parsed as HTML comment syntax.
func escapeHTMLComments(s string) string {
	s = strings.ReplaceAll(s, "<!--", "&lt;!--")
	s = strings.ReplaceAll(s, "-->", "--&gt;")
	return s
}

func commas(n int64) string {
	s := fmt.Sprintf("%d", n)
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}

func StripBlock(body string) string {
	start := strings.Index(body, Begin)
	end := strings.Index(body, End)
	if start < 0 || end < 0 || end < start {
		return body
	}
	return body[:start] + body[end+len(End):]
}
