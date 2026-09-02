package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/nakamasato/tfreview/internal/github"
	"github.com/nakamasato/tfreview/internal/render"
	"github.com/spf13/cobra"
)

var newGitHubClient = github.New

func resolveRepo(flag string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	if v := os.Getenv("GITHUB_REPOSITORY"); v != "" {
		return v, nil
	}
	return "", &exitError{code: 2, msg: "--repo is required (or set GITHUB_REPOSITORY)"}
}

func githubClient(repoFlag string) (*github.Client, error) {
	repo, err := resolveRepo(repoFlag)
	if err != nil {
		return nil, err
	}
	token := github.Token()
	if token == "" {
		return nil, &exitError{code: 2, msg: "no GitHub token: set GITHUB_TOKEN or run `gh auth login`"}
	}
	return newGitHubClient(repo, token), nil
}

func newCommentCmd() *cobra.Command {
	var resultPath, repo string
	var pr int
	var noLabel bool
	cmd := &cobra.Command{
		Use:   "comment",
		Short: "Post the review result to a pull request as one comment and a label",
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := render.LoadResult(resultPath)
			if err != nil {
				return &exitError{code: 2, msg: "read result: " + err.Error()}
			}
			client, err := githubClient(repo)
			if err != nil {
				return err
			}
			if err := client.UpsertComment(cmd.Context(), pr, render.Comment(result)); err != nil {
				return fmt.Errorf("post comment: %w", err)
			}
			if noLabel {
				return nil
			}
			if err := client.SetLabel(cmd.Context(), pr, result.Label); err != nil {
				// ラベルは補助なので、権限が無いだけならコメントを出せた時点で成功にする。
				if errors.Is(err, github.ErrLabelForbidden) {
					fmt.Fprintln(cmd.ErrOrStderr(), "warning: could not create label (needs issues: write):", err)
					return nil
				}
				return fmt.Errorf("set label: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&resultPath, "result", "tfreview-out/result.json", "result.json from `tfreview review`")
	cmd.Flags().IntVar(&pr, "pr", 0, "pull request number")
	cmd.Flags().StringVar(&repo, "repo", "", "owner/name (default: GITHUB_REPOSITORY)")
	cmd.Flags().BoolVar(&noLabel, "no-label", false, "do not touch labels")
	_ = cmd.MarkFlagRequired("pr")
	return cmd
}
