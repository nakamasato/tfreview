package main

import (
	"io"
	"os"

	"github.com/nakamasato/tfreview/internal/plan"
	"github.com/spf13/cobra"
)

func newExtractCmd() *cobra.Command {
	var showJSON, target, out string
	cmd := &cobra.Command{
		Use:   "extract",
		Short: "Reduce `terraform show -json` output to what review needs",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var raw []byte
			var err error
			if showJSON == "-" {
				raw, err = io.ReadAll(cmd.InOrStdin())
			} else {
				raw, err = os.ReadFile(showJSON)
			}
			if err != nil {
				return &exitError{code: 2, msg: "read show json: " + err.Error()}
			}
			p, err := plan.Extract(raw, target)
			if err != nil {
				return &exitError{code: 2, msg: err.Error()}
			}
			return p.Save(out)
		},
	}
	cmd.Flags().StringVar(&showJSON, "show-json", "", "path to `terraform show -json` output, or - for stdin")
	cmd.Flags().StringVar(&target, "target", "", "name of this plan's target (e.g. directory or environment)")
	cmd.Flags().StringVar(&out, "out", "", "output plan JSON path")
	_ = cmd.MarkFlagRequired("show-json")
	_ = cmd.MarkFlagRequired("target")
	_ = cmd.MarkFlagRequired("out")
	return cmd
}
