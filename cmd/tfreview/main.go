package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// exitError は exit code を運ぶ。cobra は error を返すだけなので、
// main で code に変換する。
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }

var errAny = errors.New("any")

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
	}
	root.AddCommand(newExtractCmd(), newReviewCmd())
	return root
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(exitCode(err))
	}
}
