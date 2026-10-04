package render

import (
	"fmt"
	stdhtml "html"
	"strings"

	"github.com/nakamasato/tfreview/internal/model"
)

type htmlMatrix struct {
	Perspectives []string
	Rows         []htmlMatrixRow
}

type htmlMatrixRow struct {
	Target, Resource, Action string
	Cells                    []htmlMatrixCell
}

type htmlMatrixCell struct {
	Value, Class string
}

func HTML(r *Result) (string, error) {
	var b strings.Builder
	class, meaning := htmlRisk(r)
	fmt.Fprint(&b, "<!doctype html>\n<html lang=\"en\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width, initial-scale=1\"><title>Terraform review</title><style>")
	fmt.Fprint(&b, "body{margin:0;background:#f4f6f8;color:#182230;font:15px -apple-system,BlinkMacSystemFont,Segoe UI,sans-serif}main{max-width:1100px;margin:32px auto;padding:0 20px 48px}section{background:white;border:1px solid #e4e7ec;border-radius:12px;margin:16px 0;padding:20px}h1{margin:0 0 8px}h2{margin:0 0 12px;font-size:21px}.muted{color:#667085}.summary{display:flex;flex-wrap:wrap;gap:18px;margin:16px 0}.badge{display:inline-block;border-radius:999px;padding:6px 11px;font-weight:700}.none,.safe{background:#dff5e5;color:#176b36}.medium,.unknown,.uncertain{background:#fff2cc;color:#795600}.high{background:#ffe1df;color:#a12b24}.critical,.risk{background:#a12b24;color:white}.risk{background:#ffe1df;color:#a12b24;font-weight:700}.table-wrap{overflow-x:auto}table{width:100%;border-collapse:collapse;text-align:left;font-size:14px}th,td{padding:10px;border-bottom:1px solid #eaecf0;vertical-align:top}th{color:#667085;font-size:12px;text-transform:uppercase}.matrix{text-align:center}.matrix td:first-child,.matrix th:first-child{text-align:left}.action{display:inline-block;border:1px solid #d0d5dd;border-radius:5px;padding:2px 6px;margin-right:7px;font-size:11px;font-weight:700;text-transform:uppercase}.target{display:block;color:#667085;font-size:12px;margin-top:3px}.reason{white-space:pre-wrap;overflow-wrap:anywhere}</style></head><body><main>")
	fmt.Fprintf(&b, "<h1>Terraform review</h1><p class=\"muted\">%s · %s · %s / %s</p>", stdhtml.EscapeString(r.Repo), stdhtml.EscapeString(r.JudgedAt), stdhtml.EscapeString(r.Provider), stdhtml.EscapeString(r.Model))
	fmt.Fprintf(&b, "<section><h2>Final judgment</h2><span class=\"badge %s\">%s</span><p>%s</p><div class=\"summary\"><span>Total estimated cost: <strong>$%.4f</strong></span><span>Incomplete: <strong>%t</strong></span><span>Commit: <strong>%s</strong></span></div></section>", class, stdhtml.EscapeString(r.Label), meaning, r.CostUSD, r.Incomplete, stdhtml.EscapeString(r.HeadSHA))

	fmt.Fprint(&b, "<section><h2>Phase 1: Jev scoring matrix</h2>")
	if r.Provider != "jev" {
		fmt.Fprint(&b, "<p>Jev is not the primary provider; no Jev matrix is available.</p>")
	} else {
		fmt.Fprintf(&b, "<p class=\"muted\">%s · %d calls · estimated cost $%.4f · green ≤ %.2f likely safe · red ≥ %.2f likely risk · yellow between thresholds needs individual review</p>", stdhtml.EscapeString(r.Model), r.Usage.Calls, r.CostUSD-r.DeepCostUSD, r.JevMissThreshold, r.JevHitThreshold)
		matrix := makeHTMLMatrix(r)
		if len(matrix.Rows) == 0 {
			fmt.Fprint(&b, "<p>No per-resource scores were recorded.</p>")
		} else {
			fmt.Fprint(&b, "<div class=\"table-wrap\"><table class=\"matrix\"><thead><tr><th>Terraform resource</th>")
			for _, p := range matrix.Perspectives {
				fmt.Fprintf(&b, "<th>%s</th>", stdhtml.EscapeString(p))
			}
			fmt.Fprint(&b, "</tr></thead><tbody>")
			for _, row := range matrix.Rows {
				fmt.Fprintf(&b, "<tr><td><span class=\"action\">%s</span><strong>%s</strong><span class=\"target\">%s</span></td>", stdhtml.EscapeString(row.Action), stdhtml.EscapeString(row.Resource), stdhtml.EscapeString(row.Target))
				for _, cell := range row.Cells {
					fmt.Fprintf(&b, "<td class=\"%s\">%s</td>", cell.Class, cell.Value)
				}
				fmt.Fprint(&b, "</tr>")
			}
			fmt.Fprint(&b, "</tbody></table></div>")
		}
	}
	fmt.Fprint(&b, "</section><section><h2>Phase 2: Individual LLM judgments</h2>")
	if r.DeepEnabled {
		fmt.Fprintf(&b, "<p class=\"muted\">%s · %d calls · estimated cost $%.4f</p>", stdhtml.EscapeString(r.DeepModel), r.DeepUsage.Calls, r.DeepCostUSD)
		if len(r.DeepChecks) == 0 {
			fmt.Fprintf(&b, "<p>%s</p>", stdhtml.EscapeString(htmlPhase2NotRun(r)))
		} else {
			htmlPhaseChecks(&b, r.DeepChecks)
		}
	} else {
		fmt.Fprintf(&b, "<p>%s</p>", stdhtml.EscapeString(htmlPhase2NotRun(r)))
	}
	fmt.Fprint(&b, "</section><section><h2>Checks</h2><div class=\"table-wrap\"><table><thead><tr><th>Check</th><th>Level</th><th>Verdict</th><th>Reason</th></tr></thead><tbody>")
	for _, category := range r.Categories {
		for _, check := range category.Checks {
			fmt.Fprintf(&b, "<tr><td>%s</td><td>%s</td><td>%s</td><td class=\"reason\">%s</td></tr>", stdhtml.EscapeString(check.ID), stdhtml.EscapeString(string(check.Level)), stdhtml.EscapeString(string(check.Verdict)), stdhtml.EscapeString(check.Reason))
		}
	}
	fmt.Fprint(&b, "</tbody></table></div></section></main></body></html>")
	return b.String(), nil
}

func htmlRisk(r *Result) (class, meaning string) {
	if r.Incomplete {
		return "unknown", "Some checks could not be evaluated."
	}
	switch r.Score {
	case model.SeverityCritical:
		return "critical", "Critical-severity risk findings were detected."
	case model.SeverityHigh:
		return "high", "High-severity risk findings were detected."
	case model.SeverityMedium:
		return "medium", "Medium-severity risk findings were detected."
	default:
		return "none", "No configured risk was detected."
	}
}

func makeHTMLMatrix(r *Result) htmlMatrix {
	matrix := htmlMatrix{}
	indices, rows := map[string]int{}, map[string]int{}
	for _, check := range r.PrimaryChecks {
		indices[check.ID] = len(matrix.Perspectives)
		matrix.Perspectives = append(matrix.Perspectives, check.ID)
		for _, score := range check.ResourceScores {
			key := score.Target + "\x00" + score.Resource
			index, ok := rows[key]
			if !ok {
				index = len(matrix.Rows)
				rows[key] = index
				matrix.Rows = append(matrix.Rows, htmlMatrixRow{Target: score.Target, Resource: score.Resource, Action: score.Action, Cells: make([]htmlMatrixCell, len(r.PrimaryChecks))})
			}
			class := "uncertain"
			if score.Score <= r.JevMissThreshold {
				class = "safe"
			} else if score.Score >= r.JevHitThreshold {
				class = "risk"
			}
			matrix.Rows[index].Cells[indices[check.ID]] = htmlMatrixCell{Value: fmt.Sprintf("%.2f", score.Score), Class: class}
		}
	}
	return matrix
}

func htmlPhaseChecks(b *strings.Builder, checks []PhaseCheck) {
	fmt.Fprint(b, "<div class=\"table-wrap\"><table><thead><tr><th>Perspective</th><th>Verdict</th><th>Score</th><th>Resources</th><th>Explanation</th></tr></thead><tbody>")
	for _, check := range checks {
		score := "—"
		if check.Score > 0 {
			score = fmt.Sprintf("%.2f", check.Score)
		}
		fmt.Fprintf(b, "<tr><td>%s</td><td>%s</td><td>%s</td><td>", stdhtml.EscapeString(check.ID), stdhtml.EscapeString(string(check.Verdict)), score)
		for _, resource := range check.Resources {
			fmt.Fprintf(b, "%s<br>", stdhtml.EscapeString(resource))
		}
		fmt.Fprintf(b, "</td><td class=\"reason\">%s</td></tr>", stdhtml.EscapeString(check.Reason))
	}
	fmt.Fprint(b, "</tbody></table></div>")
}

func htmlPhase2NotRun(r *Result) string {
	if !r.DeepEnabled {
		return "Not run: no second-pass LLM is configured. Set llm.deep_check.provider: anthropic to enable individual judgments."
	}
	for _, check := range r.PrimaryChecks {
		if check.Verdict == model.VerdictUnverifiable && len(check.Resources) > 0 {
			return "No individual judgment was recorded, although a second-pass provider is configured."
		}
	}
	if r.Provider == "jev" {
		return "Not run: no Jev score fell between the miss and hit thresholds for a resource requiring investigation."
	}
	return "Not run: the primary judge produced no checks requiring a second opinion."
}
