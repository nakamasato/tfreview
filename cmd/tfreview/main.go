package main

import (
	"errors"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// exitError carries an exit code. cobra only returns an error, so
// main converts it to a code.
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }

var errAny = errors.New("any")

// version is embedded at release build time via goreleaser's ldflags (-X main.version=...).
var version = "dev"

func currentVersion() string {
	if version != "dev" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return version
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	return 1
}

func newRootCmd() *cobra.Command {
	current := currentVersion()
	root := &cobra.Command{
		Use:           "tfreview",
		Short:         "Review terraform plan results against configurable risk criteria",
		Long:          "tfreview reviews terraform plan results against configurable risk criteria",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       current,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			if latest, ok := latestRelease(cmd.Context(), current); ok {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "A newer tfreview version is available: %s (you have %s). Upgrade with: go install github.com/nakamasato/tfreview/cmd/tfreview@%s\n", latest, current, latest)
			}
		},
	}
	root.AddCommand(newExtractCmd(), newReviewCmd(), newCommentCmd(), newFetchCmd(), newEvalCmd())
	return root
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(exitCode(err))
	}
}
