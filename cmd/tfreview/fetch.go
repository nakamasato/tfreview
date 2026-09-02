package main

import (
	"errors"

	"github.com/nakamasato/tfreview/internal/github"
	"github.com/spf13/cobra"
)

func newFetchCmd() *cobra.Command {
	var repo, outDir, artifact string
	var pr int
	cmd := &cobra.Command{
		Use:   "fetch",
		Short: "Download the plan JSON files a CI run uploaded for a pull request",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := githubClient(repo)
			if err != nil {
				return err
			}
			err = client.FetchPlanArtifact(cmd.Context(), pr, artifact, outDir)
			if errors.Is(err, github.ErrArtifactNotFound) {
				return &exitError{code: 1, msg: "no plan artifact for this PR; run terraform plan yourself or wait for CI: " + err.Error()}
			}
			if err != nil {
				return err
			}
			cmd.Printf("plan files written to %s\n", outDir)
			return nil
		},
	}
	cmd.Flags().IntVar(&pr, "pr", 0, "pull request number")
	cmd.Flags().StringVar(&repo, "repo", "", "owner/name (default: GITHUB_REPOSITORY, then the git origin remote)")
	cmd.Flags().StringVar(&outDir, "out-dir", "tfreview-plans", "directory to extract plan JSON into")
	cmd.Flags().StringVar(&artifact, "artifact", "tfreview-plan", "artifact name")
	_ = cmd.MarkFlagRequired("pr")
	return cmd
}
