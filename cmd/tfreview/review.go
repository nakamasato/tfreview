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
			if headSHA == "" {
				headSHA = gitHead()
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

			if failOn != "" && !result.Incomplete {
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
	cmd.Flags().StringVar(&repo, "repo", "", "owner/name, used only for links")
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

func gitHead() string {
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
