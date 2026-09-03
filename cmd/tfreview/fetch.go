package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nakamasato/tfreview/internal/planfind"
	"github.com/spf13/cobra"
)

// maxArtifactBytes skips artifacts too large to be a plan, so a fetch on a
// PR with unrelated build output doesn't download gigabytes to find nothing.
const maxArtifactBytes = 50 * 1024 * 1024

func newFetchCmd() *cobra.Command {
	var repo, outDir, artifactName string
	var pr int
	var targetPrefixes []string
	cmd := &cobra.Command{
		Use:   "fetch",
		Short: "Download the plan JSON files a CI run uploaded for a pull request",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := githubClient(repo)
			if err != nil {
				return err
			}
			artifacts, _, err := client.ListArtifacts(cmd.Context(), pr)
			if err != nil {
				return err
			}

			if artifactName == "" {
				// The action's own artifact is already a reduced plan; use it
				// as-is when present rather than reclassifying its contents.
				for _, a := range artifacts {
					if a.Name != "tfreview-plan" {
						continue
					}
					raw, err := client.DownloadArtifact(cmd.Context(), a)
					if err != nil {
						return err
					}
					if err := planfind.ExtractAll(raw, outDir); err != nil {
						return err
					}
					entries, err := os.ReadDir(outDir)
					if err != nil {
						return err
					}
					for _, e := range entries {
						cmd.Printf("wrote %s (reduced, from artifact %s)\n", filepath.Join(outDir, e.Name()), a.Name)
					}
					return nil
				}
			}

			candidates := artifacts
			if artifactName != "" {
				candidates = nil
				for _, a := range artifacts {
					if a.Name == artifactName {
						candidates = append(candidates, a)
					}
				}
			}

			seenNames := make([]string, 0, len(candidates))
			written := map[string]string{} // target -> artifact name that wrote it
			any := false
			for _, a := range candidates {
				seenNames = append(seenNames, a.Name)
				if a.Expired || a.SizeInBytes > maxArtifactBytes {
					continue
				}
				raw, err := client.DownloadArtifact(cmd.Context(), a)
				if err != nil {
					return err
				}
				found, err := planfind.FromZip(raw, a.Name, targetPrefixes)
				if err != nil {
					return err
				}
				for _, f := range found {
					any = true
					if prev, ok := written[f.Target]; ok {
						// candidates is newest-first, so the first artifact to
						// claim a target is already the newest.
						cmd.PrintErrf("warning: multiple artifacts map to target %q; keeping %s over %s\n", f.Target, prev, a.Name)
						continue
					}
					if err := os.MkdirAll(outDir, 0o755); err != nil {
						return err
					}
					path := filepath.Join(outDir, f.Target+".json")
					if err := f.Plan.Save(path); err != nil {
						return err
					}
					written[f.Target] = a.Name
					kind := "reduced"
					if f.Kind == planfind.KindRaw {
						kind = "raw"
					}
					cmd.Printf("wrote %s (%s, from artifact %s)\n", path, kind, a.Name)
				}
			}
			if any {
				return nil
			}
			if len(seenNames) == 0 {
				return &exitError{code: 1, msg: fmt.Sprintf("no plan artifact for PR #%d; no artifacts found on its CI runs; run terraform plan yourself", pr)}
			}
			return &exitError{code: 1, msg: fmt.Sprintf(
				"no plan artifact for PR #%d; artifacts found: %s — pass --artifact NAME (raw terraform show -json or tfreview extract output) or run terraform plan yourself",
				pr, strings.Join(seenNames, ", "),
			)}
		},
	}
	cmd.Flags().IntVar(&pr, "pr", 0, "pull request number")
	cmd.Flags().StringVar(&repo, "repo", "", "owner/name (default: GITHUB_REPOSITORY, then the git origin remote)")
	cmd.Flags().StringVar(&outDir, "out-dir", "tfreview-plans", "directory to extract plan JSON into")
	cmd.Flags().StringVar(&artifactName, "artifact", "", "restrict to this artifact name (default: auto-detect)")
	cmd.Flags().StringArrayVar(&targetPrefixes, "target-prefix", nil, "artifact name prefix to strip when deriving a target name (repeatable; checked before the built-in prefixes)")
	_ = cmd.MarkFlagRequired("pr")
	return cmd
}
