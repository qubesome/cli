package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qubesome/cli/internal/gateway"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The gateway belongs to the session and not to a profile, and it outlives
// the profile that started it, so its provenance is read from the record
// that launch wrote rather than inferred from whichever profiles happen to
// be active now.
func TestRecordedConfigReadsBackWhatWasWritten(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	g := gateway.Gateway{ConfigPath: filepath.Join(dir, "gateway-config")}

	require.NoError(t, os.WriteFile(g.ConfigPath, []byte("/home/u/dotfiles/qubesome.yaml\n"), 0o600))

	got, ok := g.RecordedConfig()
	assert.True(t, ok)
	assert.Equal(t, "/home/u/dotfiles/qubesome.yaml", got)
}

// No record is not an empty answer. A caller has to tell the two apart,
// because one means the provenance is unknown and the other would name a
// config called "".
func TestRecordedConfigWithNoRecord(t *testing.T) {
	t.Parallel()

	g := gateway.Gateway{ConfigPath: filepath.Join(t.TempDir(), "gateway-config")}

	got, ok := g.RecordedConfig()
	assert.False(t, ok)
	assert.Empty(t, got)
}

// A record that was created but never filled in says nothing, and must not
// read as a config whose path is empty.
func TestRecordedConfigWithAnEmptyRecord(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	g := gateway.Gateway{ConfigPath: filepath.Join(dir, "gateway-config")}

	require.NoError(t, os.WriteFile(g.ConfigPath, []byte("\n"), 0o600))

	_, ok := g.RecordedConfig()
	assert.False(t, ok)
}
