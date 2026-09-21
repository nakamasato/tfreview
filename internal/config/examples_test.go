package config

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExamplesAreValid(t *testing.T) {
	for _, name := range []string{"aws.yaml", "gcp.yaml"} {
		t.Run(name, func(t *testing.T) {
			c, err := Load(filepath.Join("..", "..", "examples", name))
			require.NoError(t, err)
			require.NotEmpty(t, c.Aspects)
			ck, ok := c.Check("resource-deletion")
			require.True(t, ok)
			// The examples are what users copy, so the scored phrasing has to survive
			// YAML's reading of a bare `true:` as a boolean key.
			require.Contains(t, ck.Instructions, "`focus`")
			require.NotEmpty(t, ck.Criteria.True)
			require.NotEmpty(t, ck.Criteria.False)
		})
	}
}
