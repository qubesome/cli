package cmd

import (
	"embed"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed */*
var cmdFS embed.FS

func TestCommandWiring(t *testing.T) {
	entries, err := cmdFS.ReadDir(".")
	require.NoError(t, err)
	require.Greater(t, len(entries), 0)

	c := newConsole()
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		got := c.Command(entry.Name())
		assert.True(t, got, "command %s is not configured", entry.Name())
	}
}
