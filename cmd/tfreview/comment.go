package main

import (
	"errors"
	"fmt"

	"github.com/nakamasato/tfreview/internal/github"
	"github.com/nakamasato/tfreview/internal/render"
	"github.com/spf13/cobra"
)

var newGitHubClient = github.New

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
				// The label is just a convenience; if we lack permission for it but
				// already posted the comment, treat the run as successful.
				if errors.Is(err, github.ErrLabelForbidden) {
					cmd.PrintErrln("warning: could not create label (needs issues: write):", err)
					return nil
				}
				return fmt.Errorf("set label: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&resultPath, "result", "tfreview-out/result.json", "result.json from `tfreview review`")
	cmd.Flags().IntVar(&pr, "pr", 0, "pull request number")
	cmd.Flags().StringVar(&repo, "repo", "", "owner/name (default: GITHUB_REPOSITORY, then the git origin remote)")
	cmd.Flags().BoolVar(&noLabel, "no-label", false, "do not touch labels")
	_ = cmd.MarkFlagRequired("pr")
	return cmd
}
