package main

import (
	"context"
	"fmt"
	"html/template"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nakamasato/tfreview/internal/render"
	"github.com/spf13/cobra"
)

func newEvalCmd() *cobra.Command {
	var prs []int
	var repo, configPath, provider, modelName, deepDive, outPath, format string
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Review historical pull request plans and write a local report",
		Long:  "Fetch saved plan artifacts for pull requests, review them with the current configuration, and write a local Markdown or HTML report. Plans are stored temporarily and removed after the run.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if format != "markdown" && format != "html" {
				return &exitError{code: 2, msg: "--format must be markdown or html"}
			}
			if len(prs) == 0 {
				return &exitError{code: 2, msg: "at least one --pr is required"}
			}
			for _, pr := range prs {
				if pr < 1 {
					return &exitError{code: 2, msg: "pull request numbers must be positive"}
				}
			}
			resolvedRepo, err := resolveRepo(repo)
			if err != nil {
				return err
			}
			stamp := time.Now().Format("20060102-150405")
			if outPath == "" {
				ext := ".md"
				if format == "html" {
					ext = ".html"
				}
				outPath = "tfreview-eval-" + stamp + ext
			}
			absOut, err := filepath.Abs(outPath)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(absOut), 0o755); err != nil {
				return err
			}
			tmp, err := os.MkdirTemp("", "tfreview-pr-eval-")
			if err != nil {
				return err
			}
			defer os.RemoveAll(tmp)

			executable, err := os.Executable()
			if err != nil {
				return err
			}
			rows := make([]evalReportRow, 0, len(prs))
			for _, pr := range prs {
				prDir := filepath.Join(tmp, fmt.Sprintf("pr-%d", pr))
				plansDir := filepath.Join(prDir, "plans")
				if err := runTfreview(cmd.Context(), executable, cmd.ErrOrStderr(), "fetch", "--pr", fmt.Sprint(pr), "--repo", resolvedRepo, "--out-dir", plansDir); err != nil {
					rows = append(rows, evalReportRow{PR: pr, Error: "plan fetch failed: " + err.Error()})
					continue
				}
				entries, err := os.ReadDir(plansDir)
				if err != nil {
					rows = append(rows, evalReportRow{PR: pr, Error: "could not read fetched plans: " + err.Error()})
					continue
				}
				args := []string{"review", "--config", configPath, "--out-dir", filepath.Join(prDir, "review"), "--repo", resolvedRepo, "--format", "json"}
				planCount := 0
				for _, entry := range entries {
					if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
						args = append(args, "--plan", filepath.Join(plansDir, entry.Name()))
						planCount++
					}
				}
				if planCount == 0 {
					rows = append(rows, evalReportRow{PR: pr, Error: "no plan JSON files found"})
					continue
				}
				if provider != "" {
					args = append(args, "--provider", provider)
				}
				if modelName != "" {
					args = append(args, "--model", modelName)
				}
				if deepDive != "" {
					args = append(args, "--deep-dive", deepDive)
				}
				reviewErr := runTfreview(cmd.Context(), executable, cmd.ErrOrStderr(), args...)
				result, err := render.LoadResult(filepath.Join(prDir, "review", "result.json"))
				if err != nil {
					message := "could not read review result: " + err.Error()
					if reviewErr != nil {
						message = "review failed: " + reviewErr.Error() + "; " + message
					}
					rows = append(rows, evalReportRow{PR: pr, Error: message})
					continue
				}
				row := evalReportRow{PR: pr, Result: result, Matrix: makeJevMatrix(result)}
				row.RiskClass, row.RiskLabel, row.RiskMeaning = evalRisk(result)
				if reviewErr != nil {
					row.Note = "review process returned an error after writing its result: " + reviewErr.Error()
				}
				rows = append(rows, row)
			}

			var report string
			switch format {
			case "markdown":
				report = renderEvalReport(resolvedRepo, configPath, rows)
			case "html":
				report, err = renderEvalHTML(resolvedRepo, configPath, rows)
				if err != nil {
					return fmt.Errorf("render HTML report: %w", err)
				}
			default:
				return &exitError{code: 2, msg: "--format must be markdown or html"}
			}
			if err := os.WriteFile(absOut, []byte(report), 0o600); err != nil {
				return err
			}
			cmd.Printf("wrote local report %s\n", absOut)
			return nil
		},
	}
	cmd.Flags().IntSliceVar(&prs, "pr", nil, "pull request number (repeatable or comma-separated)")
	cmd.Flags().StringVar(&repo, "repo", "", "owner/name (default: GITHUB_REPOSITORY, then the git origin remote)")
	cmd.Flags().StringVar(&configPath, "config", ".tfreview.yaml", "config path")
	cmd.Flags().StringVar(&provider, "provider", "", "override llm.provider")
	cmd.Flags().StringVar(&modelName, "model", "claude-sonnet-5-5", "override llm.model (default: claude-sonnet-5-5)")
	cmd.Flags().StringVar(&deepDive, "deep-dive", "", "override llm.deep_dive (anthropic)")
	cmd.Flags().StringVar(&format, "format", "markdown", "report format (markdown|html)")
	cmd.Flags().StringVar(&outPath, "out", "", "local report path (default: tfreview-eval-<timestamp> with format extension)")
	return cmd
}

type evalReportRow struct {
	PR          int
	Result      *render.Result
	Matrix      evalJevMatrix
	RiskClass   string
	RiskLabel   string
	RiskMeaning string
	Error       string
	Note        string
}

type evalJevPerspective struct{ ID string }

type evalJevCell struct {
	Value string
	Class string
}

type evalJevMatrixRow struct {
	Target   string
	Resource string
	Action   string
	Cells    []evalJevCell
}

type evalJevMatrix struct {
	Enabled       bool
	Perspectives  []evalJevPerspective
	Rows          []evalJevMatrixRow
	Calls         int
	InputTokens   int64
	OutputTokens  int64
	CostUSD       float64
	HitThreshold  float64
	MissThreshold float64
}

func makeJevMatrix(result *render.Result) evalJevMatrix {
	matrix := evalJevMatrix{
		Enabled: result.Provider == "jev",
		Calls:   result.Usage.Calls, InputTokens: result.Usage.InputTokens, OutputTokens: result.Usage.OutputTokens,
		CostUSD:      result.CostUSD - result.DeepCostUSD,
		HitThreshold: result.JevHitThreshold, MissThreshold: result.JevMissThreshold,
	}
	if !matrix.Enabled {
		return matrix
	}
	indices := map[string]int{}
	rowIndices := map[string]int{}
	for _, check := range result.PrimaryChecks {
		indices[check.ID] = len(matrix.Perspectives)
		matrix.Perspectives = append(matrix.Perspectives, evalJevPerspective{ID: check.ID})
		for _, score := range check.ResourceScores {
			key := score.Target + "\x00" + score.Resource
			rowIndex, ok := rowIndices[key]
			if !ok {
				rowIndex = len(matrix.Rows)
				rowIndices[key] = rowIndex
				cells := make([]evalJevCell, len(result.PrimaryChecks))
				matrix.Rows = append(matrix.Rows, evalJevMatrixRow{Target: score.Target, Resource: score.Resource, Action: score.Action, Cells: cells})
			}
			class := "uncertain"
			if score.Score <= matrix.MissThreshold {
				class = "safe"
			} else if score.Score >= matrix.HitThreshold {
				class = "risk"
			}
			matrix.Rows[rowIndex].Cells[indices[check.ID]] = evalJevCell{Value: fmt.Sprintf("%.2f", score.Score), Class: class}
		}
	}
	return matrix
}

func evalRisk(result *render.Result) (class, label, meaning string) {
	if result.Incomplete {
		return "unknown", "Incomplete", "Some checks were not evaluated"
	}
	switch result.Score {
	case "critical":
		return "critical", "Critical risk", "Critical-severity findings were detected"
	case "high":
		return "high", "High risk", "High-severity findings were detected"
	case "medium":
		return "medium", "Medium risk", "Medium-severity findings were detected"
	default:
		return "none", "No risk detected", "No configured risk was detected"
	}
}

func runTfreview(ctx context.Context, executable string, stderr io.Writer, args ...string) error {
	child := exec.CommandContext(ctx, executable, args...)
	child.Env = append(os.Environ(), "TFREVIEW_NO_UPDATE_CHECK=1")
	child.Stderr = stderr
	return child.Run()
}

func renderEvalReport(repo, configPath string, rows []evalReportRow) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Historical PR review report\n\n- Repository: `%s`\n- Generated: %s\n- Config: `%s`\n- Plan files were stored temporarily and removed after review.\n- This report can contain sensitive resource names and verdict reasons; review it before sharing.\n\n", repo, time.Now().UTC().Format(time.RFC3339), configPath)
	b.WriteString("| PR | Result | Provider / model | Cost (USD) |\n| ---: | --- | --- | ---: |\n")
	for _, row := range rows {
		if row.Error != "" {
			fmt.Fprintf(&b, "| #%d | error | — | — |\n", row.PR)
			continue
		}
		fmt.Fprintf(&b, "| #%d | `%s` | %s / %s | %.4f |\n", row.PR, row.Result.Label, row.Result.Provider, row.Result.Model, row.Result.CostUSD)
	}
	for _, row := range rows {
		if row.Error != "" {
			fmt.Fprintf(&b, "\n## PR #%d\n\nCould not review this PR: %s\n", row.PR, strings.ReplaceAll(row.Error, "\n", " "))
			continue
		}
		fmt.Fprintf(&b, "\n## PR #%d\n\nScore: `%s`; incomplete: `%t`\n\n", row.PR, row.Result.Score, row.Result.Incomplete)
		if row.Note != "" {
			fmt.Fprintf(&b, "Review warning: %s\n\n", strings.ReplaceAll(row.Note, "\n", " "))
		}
		b.WriteString("| Check | Level | Verdict | Resources | Reason |\n| --- | --- | --- | --- | --- |\n")
		for _, category := range row.Result.Categories {
			for _, check := range category.Checks {
				resources := strings.ReplaceAll(strings.Join(check.Resources, ", "), "|", "\\|")
				reason := strings.ReplaceAll(strings.ReplaceAll(check.Reason, "|", "\\|"), "\n", " ")
				fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s |\n", check.ID, check.Level, check.Verdict, resources, reason)
			}
		}
	}
	return b.String()
}

type evalHTMLData struct {
	Repo      string
	Config    string
	Generated string
	Summary   evalHTMLSummary
	Rows      []evalReportRow
}

type evalHTMLSummary struct {
	PRCount   int
	TotalCost float64
	Statuses  []evalHTMLStatus
}

type evalHTMLStatus struct {
	Label   string
	Class   string
	Count   int
	Meaning string
}

func summarizeEvalRows(rows []evalReportRow) evalHTMLSummary {
	counts := map[string]int{}
	summary := evalHTMLSummary{PRCount: len(rows)}
	for _, row := range rows {
		if row.Error != "" {
			counts["error"]++
			continue
		}
		summary.TotalCost += row.Result.CostUSD
		counts[row.RiskClass]++
	}
	for _, status := range []evalHTMLStatus{
		{Label: "Critical risk", Class: "critical", Meaning: "Critical-severity findings"},
		{Label: "High risk", Class: "high", Meaning: "High-severity findings"},
		{Label: "Medium risk", Class: "medium", Meaning: "Medium-severity findings"},
		{Label: "No risk detected", Class: "none", Meaning: "No configured risk detected"},
		{Label: "Incomplete", Class: "unknown", Meaning: "Some checks were not evaluated"},
		{Label: "Review failed", Class: "error", Meaning: "Plan fetch or review failed"},
	} {
		status.Count = counts[status.Class]
		summary.Statuses = append(summary.Statuses, status)
	}
	return summary
}

func phase2NotRunReason(result *render.Result) string {
	if !result.DeepEnabled {
		return "Phase 2 was not run: no second-pass provider is configured. Pass --deep-dive anthropic or set llm.deep_dive: anthropic."
	}
	if result.Provider == "jev" {
		if len(result.PrimaryChecks) == 0 {
			return "Phase 2 was not run: Jev produced no eligible check scores, usually because there were no changed resources or no scoring checks applied."
		}
		return "Phase 2 was not run: no Jev result landed between the miss and hit thresholds with a resource to investigate. Scores below the miss threshold are treated as low risk; scores at or above the hit threshold are treated as risk."
	}
	return "Phase 2 was not run: the primary judge produced no checks requiring a second opinion."
}

var evalHTMLTemplate = template.Must(template.New("eval-report").Funcs(template.FuncMap{
	"phase2NotRunReason": phase2NotRunReason,
}).Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Historical PR review report</title>
  <style>
    :root { color-scheme: light; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; color: #182230; background: #f4f6f8; }
    body { margin: 0; }
    main { max-width: 1120px; margin: 0 auto; padding: 36px 24px 64px; }
    h1 { margin: 0 0 8px; font-size: 30px; }
    h2 { margin: 0; font-size: 22px; }
    h3 { margin: 26px 0 10px; font-size: 16px; }
    .muted { color: #667085; font-size: 14px; }
    .meta, .pr-card { background: #fff; border: 1px solid #e4e7ec; border-radius: 12px; }
    .meta { margin: 24px 0; padding: 16px 20px; display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 12px; }
    .meta strong { display: block; margin-bottom: 4px; font-size: 12px; color: #667085; text-transform: uppercase; letter-spacing: .04em; }
    .pr-card { margin: 18px 0; overflow: hidden; }
    .pr-head { padding: 18px 20px; display: flex; align-items: center; justify-content: space-between; gap: 12px; border-bottom: 1px solid #e4e7ec; }
    .score { display: inline-block; border-radius: 999px; padding: 5px 10px; background: #eef2f6; font-size: 13px; font-weight: 650; white-space: nowrap; }
    .score.unknown { background: #fff1cc; color: #7a4d00; }
    .score.none { background: #dff5e5; color: #176b36; }
    .score.medium { background: #fff2cc; color: #795600; }
    .score.high { background: #ffe1df; color: #a12b24; }
    .score.critical { background: #a12b24; color: #fff; }
    .score.error { background: #fff0f0; color: #912018; }
    .summary { padding: 14px 20px; display: flex; flex-wrap: wrap; gap: 20px; color: #475467; font-size: 14px; }
    .warning, .error { margin: 12px 20px; padding: 12px 14px; border-radius: 8px; background: #fff7e6; color: #7a4d00; }
    .error { background: #fff0f0; color: #912018; }
    .table-wrap { overflow-x: auto; padding: 0 20px 20px; }
    table { width: 100%; border-collapse: collapse; font-size: 14px; text-align: left; }
    th { color: #667085; font-size: 12px; text-transform: uppercase; letter-spacing: .04em; }
    th, td { border-bottom: 1px solid #eaecf0; padding: 11px 10px; vertical-align: top; }
    td:first-child, th:first-child { padding-left: 0; }
    td:last-child, th:last-child { padding-right: 0; }
    .verdict { font-weight: 650; white-space: nowrap; }
    .reason { min-width: 260px; white-space: pre-wrap; overflow-wrap: anywhere; }
    .resources { min-width: 180px; overflow-wrap: anywhere; color: #475467; }
    .phase { margin: 18px 20px 8px; }
    .phase-meta { margin: 0 20px 14px; color: #475467; font-size: 14px; }
    .matrix { text-align: center; }
    .matrix th:first-child, .matrix td:first-child { text-align: left; }
    .matrix .score-safe { background: #dff5e5; color: #176b36; font-weight: 700; }
    .matrix .score-risk { background: #ffe1df; color: #a12b24; font-weight: 700; }
    .matrix .score-uncertain { background: #fff2cc; color: #795600; font-weight: 700; }
    .legend { display: flex; gap: 14px; flex-wrap: wrap; margin: 8px 20px 16px; color: #475467; font-size: 13px; }
    .legend span { border-radius: 5px; padding: 4px 8px; }
    .target { display: block; color: #667085; font-size: 12px; margin-top: 3px; }
    .action { display: inline-block; margin-right: 7px; border: 1px solid #d0d5dd; border-radius: 5px; padding: 2px 6px; color: #344054; font-size: 11px; font-weight: 700; text-transform: uppercase; white-space: nowrap; }
    .score-label { display: block; margin-top: 4px; font-size: 12px; font-weight: 400; }
    .final-summary { margin: 22px 0; padding: 20px; background: #fff; border: 1px solid #e4e7ec; border-radius: 12px; }
    .final-summary h2 { margin-bottom: 8px; }
    .status-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(145px, 1fr)); gap: 10px; margin: 18px 0; }
    .status-card { padding: 12px; border: 1px solid #eaecf0; border-radius: 8px; }
    .status-card strong { display: block; margin-top: 6px; font-size: 22px; }
    .final-table { margin-top: 16px; }
    @media (max-width: 640px) { main { padding: 24px 12px 40px; } .pr-head { align-items: flex-start; flex-direction: column; } }
  </style>
</head>
<body>
<main>
  <h1>Historical PR review report</h1>
  <p class="muted">Review of saved Terraform plans using the current tfreview configuration.</p>
  <section class="meta" aria-label="Report details">
    <div><strong>Repository</strong>{{.Repo}}</div>
    <div><strong>Config</strong>{{.Config}}</div>
    <div><strong>Generated</strong>{{.Generated}}</div>
  </section>
  <p class="muted">Plan files were stored temporarily and removed after review. This report may contain sensitive resource names and verdict reasons; review it before sharing.</p>
  <section class="final-summary" aria-label="Final judgment summary">
    <h2>Final judgment summary</h2>
    <p class="muted">High and critical mean risk findings were detected. “No risk detected” means no configured risk was found. “Incomplete” means some checks could not be evaluated.</p>
    <div class="status-grid">{{range .Summary.Statuses}}<div class="status-card"><span class="score {{.Class}}">{{.Label}}</span><strong>{{.Count}}</strong><span class="muted">{{.Meaning}}</span></div>{{end}}</div>
    <p><strong>{{.Summary.PRCount}}</strong> PRs · estimated total cost <strong>${{printf "%.4f" .Summary.TotalCost}}</strong></p>
    <div class="table-wrap final-table"><table>
      <thead><tr><th>PR</th><th>Final judgment</th><th>What it means</th></tr></thead>
      <tbody>{{range .Rows}}<tr><td>#{{.PR}}</td>
      {{if .Error}}<td><span class="score error">Review failed</span></td><td>{{.Error}}</td>
      {{else}}<td><span class="score {{.RiskClass}}">{{.RiskLabel}}<span class="score-label">{{.Result.Label}}</span></span></td><td>{{.RiskMeaning}}</td>{{end}}
      </tr>{{end}}</tbody>
    </table></div>
  </section>
  {{range .Rows}}
  <article class="pr-card">
    <header class="pr-head">
      <h2>PR #{{.PR}}</h2>
      {{if .Result}}<span class="score {{.RiskClass}}">{{.RiskLabel}}<span class="score-label">{{.Result.Label}}</span></span>{{else}}<span class="score error">Review failed</span>{{end}}
    </header>
    {{if .Error}}
      <div class="error">{{.Error}}</div>
    {{else}}
      <div class="summary">
        <span>Final risk: <strong>{{.RiskLabel}}</strong> — {{.RiskMeaning}}</span>
        <span>Provider / model: <strong>{{.Result.Provider}} / {{.Result.Model}}</strong></span>
        <span>Estimated cost: <strong>${{printf "%.4f" .Result.CostUSD}}</strong></span>
        <span>Incomplete: <strong>{{.Result.Incomplete}}</strong></span>
      </div>
      {{if .Note}}<div class="warning">{{.Note}}</div>{{end}}
      <h3 class="phase">Phase 1: Jev scoring matrix</h3>
      {{if .Matrix.Enabled}}
        <p class="phase-meta">{{.Result.Model}} · {{.Matrix.Calls}} calls · {{.Matrix.InputTokens}} input tokens · estimated cost ${{printf "%.4f" .Matrix.CostUSD}} · green ≤ {{printf "%.2f" .Matrix.MissThreshold}} miss threshold · red ≥ {{printf "%.2f" .Matrix.HitThreshold}} hit threshold</p>
        {{if .Matrix.Rows}}
          <div class="legend"><span class="score-safe">Low score: likely safe</span><span class="score-uncertain">Middle score: needs review</span><span class="score-risk">High score: likely risk</span></div>
          <div class="table-wrap"><table class="matrix">
            <thead><tr><th>Terraform resource</th>{{range .Matrix.Perspectives}}<th>{{.ID}}</th>{{end}}</tr></thead>
            <tbody>{{range .Matrix.Rows}}<tr>
              <td><span class="action">{{.Action}}</span><strong>{{.Resource}}</strong><span class="target">{{.Target}}</span></td>
              {{range .Cells}}<td class="score-{{.Class}}">{{.Value}}</td>{{end}}
            </tr>{{end}}</tbody>
          </table></div>
        {{else}}
          <p class="phase-meta">No per-resource scores were recorded for this PR.</p>
        {{end}}
      {{else}}
        <p class="phase-meta">Jev is not configured as the primary provider. Set <code>llm.provider: jev</code> to produce this matrix.</p>
      {{end}}

      <h3 class="phase">Phase 2: Individual LLM judgments</h3>
      {{if .Result.DeepEnabled}}
        <p class="phase-meta">{{.Result.DeepModel}} · {{.Result.DeepUsage.Calls}} calls · estimated cost ${{printf "%.4f" .Result.DeepCostUSD}}</p>
        {{if .Result.DeepChecks}}
          <div class="table-wrap"><table>
            <thead><tr><th>Perspective</th><th>Verdict</th><th>Score</th><th>Resources</th><th>Explanation</th></tr></thead>
            <tbody>{{range .Result.DeepChecks}}<tr>
              <td><strong>{{.ID}}</strong></td><td class="verdict">{{.Verdict}}</td><td>{{if .Score}}{{printf "%.2f" .Score}}{{else}}—{{end}}</td>
              <td class="resources">{{range $i, $resource := .Resources}}{{if $i}}<br>{{end}}{{$resource}}{{end}}</td>
              <td class="reason">{{.Reason}}</td>
            </tr>{{end}}</tbody>
          </table></div>
        {{else}}
          <p class="phase-meta">{{phase2NotRunReason .Result}}</p>
        {{end}}
      {{else}}
        <p class="phase-meta">{{phase2NotRunReason .Result}}</p>
      {{end}}
    {{end}}
  </article>
  {{end}}
</main>
</body>
</html>
`))

func renderEvalHTML(repo, configPath string, rows []evalReportRow) (string, error) {
	var b strings.Builder
	err := evalHTMLTemplate.Execute(&b, evalHTMLData{
		Repo: repo, Config: configPath, Generated: time.Now().UTC().Format(time.RFC3339),
		Summary: summarizeEvalRows(rows), Rows: rows,
	})
	if err != nil {
		return "", err
	}
	return b.String(), nil
}
