package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
		plans      []string
		configPath string
		stateIn    string
		outDir     string
		headSHA    string
		repo       string
		failOn     string
		ruleOnly   bool
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
			for _, line := range unmatchedTargetWarnings(cfg, ps) {
				cmd.PrintErrln(line)
			}

			// --fail-on-rule-only exists to block deterministically regardless of
			// LLM availability, so when ruleOnly is set, ignore result.Incomplete
			// (which stems from a skipped LLM verdict) and judge on RuleScore alone.
			// The default (ruleOnly=false) keeps skipping fail-on on Incomplete.
			if failOn != "" && (ruleOnly || !result.Incomplete) {
				score := result.Score
				if ruleOnly {
					score = result.RuleScore
				}
				if model.LevelAtLeast(score, failLevel) {
					if ruleOnly && result.Incomplete {
						// The posted label/comment (built from the combined score) still
						// say result.Label here, e.g. "tfreview:unknown", because some LLM
						// checks were skipped. Without this, a reviewer sees a red CI check
						// next to a comment that looks like nothing was judged yet.
						cmd.PrintErrf("note: failing on the rule-only score (%s) even though some LLM checks were skipped; the posted label/comment still read %q\n", score, result.Label)
					}
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
	cmd.Flags().BoolVar(&ruleOnly, "fail-on-rule-only", false, "with --fail-on, count only deterministic (match) verdicts")
	return cmd
}

// If there's no config, fall back to the built-in defaults. If a config exists
// but is broken, exit 2: running with zero checks would make every PR look
// green, so failing loudly is the correct behavior.
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

// unmatchedTargetWarnings flags match.targets entries that name a target none of
// the loaded plans has. Nothing in cfg alone can tell a typo from a target
// that simply wasn't part of this run (e.g. reviewing only the targets that
// changed), so this only compares against the plans actually passed in and
// never affects the verdict — it is purely a "check for a typo" hint. With no
// plans loaded at all there is nothing meaningful to compare against, so no
// warning is produced (see TestReviewNoPlansIsNone).
func unmatchedTargetWarnings(cfg *config.Config, ps []*plan.Plan) []string {
	if len(ps) == 0 {
		return nil
	}
	loaded := map[string]bool{}
	for _, p := range ps {
		if p.Target != "" {
			loaded[p.Target] = true
		}
	}
	loadedList := make([]string, 0, len(loaded))
	for t := range loaded {
		loadedList = append(loadedList, t)
	}
	sort.Strings(loadedList)

	var order []string
	byTarget := map[string][]string{}
	for _, cat := range cfg.Categories {
		for _, ck := range cat.Checks {
			for _, t := range ck.Match.Targets {
				if loaded[t] {
					continue
				}
				if _, ok := byTarget[t]; !ok {
					order = append(order, t)
				}
				byTarget[t] = append(byTarget[t], ck.ID)
			}
		}
	}
	sort.Strings(order)

	lines := make([]string, 0, len(order))
	for _, t := range order {
		lines = append(lines, fmt.Sprintf(
			"warning: match.targets %q (checks: %s) does not match any of the loaded targets: %s — check for a typo",
			t, strings.Join(byTarget[t], ", "), strings.Join(loadedList, ", ")))
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
