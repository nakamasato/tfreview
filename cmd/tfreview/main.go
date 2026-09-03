package main

import (
	"errors"
	"fmt"
	"os"

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
	root := &cobra.Command{
		Use:           "tfreview",
		Short:         "Review terraform plan results against configurable risk criteria",
		Long:          "tfreview reviews terraform plan results against configurable risk criteria",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
	}
	root.AddCommand(newExtractCmd(), newReviewCmd(), newCommentCmd(), newFetchCmd())
	return root
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(exitCode(err))
	}
}
