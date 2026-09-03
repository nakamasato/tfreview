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
			require.NotEmpty(t, c.Categories)
			_, ok := c.Check("delete-or-replace")
			require.True(t, ok)
		})
	}
}
