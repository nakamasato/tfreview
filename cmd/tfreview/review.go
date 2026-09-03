package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nakamasato/tfreview/internal/config"
	"github.com/nakamasato/tfreview/internal/judge"
	"github.com/nakamasato/tfreview/internal/llm"
	"github.com/nakamasato/tfreview/internal/model"
	"github.com/nakamasato/tfreview/internal/plan"
	"github.com/nakamasato/tfreview/internal/render"
	"github.com/nakamasato/tfreview/internal/state"
	"github.com/spf13/cobra"
)

var now = time.Now

func newReviewCmd() *cobra.Command {
	var (
		plans       []string
		configPath  string
		stateIn     string
		outDir      string
		headSHA     string
		repo        string
		failOn      string
		machineOnly bool
	)
	cmd := &cobra.Command{
		Use:   "review",
		Short: "Judge plan JSON files against the configured checks",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var failLevel model.Level
			if failOn != "" {
				l, err := model.ParseLevel(failOn)
				if err != nil {
					return &exitError{code: 2, msg: "--fail-on: " + err.Error()}
				}
				failLevel = l
			}

			cfg, err := loadConfig(configPath)
			if err != nil {
				return err
			}
			var ps []*plan.Plan
			for _, path := range plans {
				p, err := plan.Load(path)
				if err != nil {
					return &exitError{code: 2, msg: "read plan " + path + ": " + err.Error()}
				}
				ps = append(ps, p)
			}
			provider, err := newProvider(cfg)
			if err != nil {
				return &exitError{code: 2, msg: err.Error()}
			}
			if cfg.LLM.Provider == "anthropic" && os.Getenv("ANTHROPIC_API_KEY") == "" {
				// report-only tool: a missing key must not stop the run, but a
				// silent tfreview:unknown with no explanation is worse than noise.
				cmd.PrintErrln("warning: ANTHROPIC_API_KEY is not set; LLM checks will be skipped and the result will be tfreview:unknown")
			}
			if headSHA == "" {
				headSHA = gitHead()
			}
			if repo == "" {
				repo = os.Getenv("GITHUB_REPOSITORY")
			}
			if repo == "" {
				repo = gitRemoteRepo()
			}

			out, err := judge.Run(cmd.Context(), judge.Input{Config: cfg, Plans: ps, Provider: provider, Prev: state.Load(stateIn), HeadSHA: headSHA})
			if err != nil {
				return err
			}
			result := render.Build(cfg, out, render.Meta{
				HeadSHA: headSHA, JudgedAt: now().UTC().Format(time.RFC3339), Repo: repo,
				ConfigPath: configPathForLink(configPath), Model: provider.Model(), Pricing: llm.PricingFromMap(cfg.LLM.Pricing),
			})

			if err := os.MkdirAll(outDir, 0o755); err != nil {
				return err
			}
			if err := result.Save(filepath.Join(outDir, "result.json")); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(outDir, "comment.md"), []byte(render.Comment(result)), 0o644); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(outDir, "label.txt"), []byte(result.Label+"\n"), 0o644); err != nil {
				return err
			}
			if err := out.State.Save(filepath.Join(outDir, "state.json")); err != nil {
				return err
			}
			cmd.Printf("%s (%s)\n", result.Label, outDir)
			if result.Incomplete {
				for _, line := range skippedSummaryLines(result) {
					cmd.PrintErrln(line)
				}
			}

			// --fail-on-machine-only exists to block deterministically regardless of
			// LLM availability, so when machineOnly is set, ignore result.Incomplete
			// (which stems from a skipped LLM verdict) and judge on MachineScore alone.
			// The default (machineOnly=false) keeps skipping fail-on on Incomplete.
			if failOn != "" && (machineOnly || !result.Incomplete) {
				score := result.Score
				if machineOnly {
					score = result.MachineScore
				}
				if model.LevelAtLeast(score, failLevel) {
					return &exitError{code: 1, msg: "risk " + string(score) + " reached --fail-on " + failOn}
				}
			}
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&plans, "plan", nil, "plan JSON from `tfreview extract` (repeatable)")
	cmd.Flags().StringVar(&configPath, "config", ".tfreview.yaml", "config path")
	cmd.Flags().StringVar(&stateIn, "state-in", "", "state.json from the previous run")
	cmd.Flags().StringVar(&outDir, "out-dir", "tfreview-out", "output directory")
	cmd.Flags().StringVar(&headSHA, "head-sha", "", "commit being judged (default: git rev-parse HEAD)")
	cmd.Flags().StringVar(&repo, "repo", "", "owner/name, used only for links (default: GITHUB_REPOSITORY, then the git origin remote)")
	cmd.Flags().StringVar(&failOn, "fail-on", "", "exit 1 when the score reaches this level (medium|high|critical)")
	cmd.Flags().BoolVar(&machineOnly, "fail-on-machine-only", false, "with --fail-on, count only deterministic (match) verdicts")
	return cmd
}

// config が無ければ組み込みデフォルトで動く。あって壊れていれば exit 2:
// 観点ゼロで走ると全 PR が緑になるので、落とすのが正しい。
func loadConfig(path string) (*config.Config, error) {
	cfg, err := config.Load(path)
	if err == nil {
		return cfg, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return config.Parse(nil)
	}
	return nil, &exitError{code: 2, msg: err.Error()}
}

func configPathForLink(path string) string {
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return filepath.ToSlash(path)
}

// skippedSummaryLines groups skipped checks by their (truncated) reason so a
// run without credentials prints one line per cause instead of one line per
// check, which for a real config can be dozens of lines saying the same thing.
func skippedSummaryLines(result *render.Result) []string {
	var order []string
	byReason := map[string][]string{}
	for _, cat := range result.Categories {
		for _, ck := range cat.Checks {
			if ck.Verdict != model.VerdictSkipped {
				continue
			}
			reason := truncateReason(ck.Reason)
			if _, ok := byReason[reason]; !ok {
				order = append(order, reason)
			}
			byReason[reason] = append(byReason[reason], ck.ID)
		}
	}
	lines := make([]string, 0, len(order))
	for _, reason := range order {
		lines = append(lines, "skipped: "+strings.Join(byReason[reason], ", ")+" — "+reason)
	}
	return lines
}

func truncateReason(reason string) string {
	if i := strings.IndexByte(reason, '\n'); i >= 0 {
		reason = reason[:i]
	}
	if len(reason) > 200 {
		reason = reason[:200]
	}
	return reason
}

func gitHead() string {
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
