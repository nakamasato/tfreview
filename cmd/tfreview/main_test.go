package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRootHelpListsSubcommands(t *testing.T) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--help"})
	require.NoError(t, cmd.Execute())
	require.Contains(t, out.String(), "tfreview")
}

func TestExitError(t *testing.T) {
	err := &exitError{code: 2, msg: "bad config"}
	require.Equal(t, "bad config", err.Error())
	require.Equal(t, 2, exitCode(err))
	require.Equal(t, 0, exitCode(nil))
	require.Equal(t, 1, exitCode(errAny))
}
