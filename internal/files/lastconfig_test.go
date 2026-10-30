package files

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The record is read back from another working directory than the one it
// was written in, which is the whole point of it, so a relative path has
// to be resolved before it is stored.
func TestARelativeConfigIsRememberedAsAnAbsolutePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "qubesome.config"), []byte("{}"), 0o600))
	t.Chdir(dir)

	RememberConfig("qubesome.config")

	got, ok := RememberedConfig()
	require.True(t, ok)
	assert.Equal(t, filepath.Join(dir, "qubesome.config"), got)
}

// A repository that was moved or removed leaves a record naming nothing.
// Offering it would send the user after a path that cannot work.
func TestAConfigThatHasGoneIsNotRemembered(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := t.TempDir()
	path := filepath.Join(dir, "qubesome.config")
	require.NoError(t, os.WriteFile(path, []byte("{}"), 0o600))

	RememberConfig(path)
	require.NoError(t, os.Remove(path))

	_, ok := RememberedConfig()
	assert.False(t, ok)
}

func TestNothingIsRememberedOnAHostThatHasOpenedNoConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	_, ok := RememberedConfig()
	assert.False(t, ok)
}

// A home directory that cannot be written to is a reason to go without
// the convenience, not to fail the command the user asked for.
func TestRememberingIsQuietWhenItCannotBeWritten(t *testing.T) {
	home := filepath.Join(t.TempDir(), "unwritable")
	require.NoError(t, os.Mkdir(home, 0o500))
	t.Setenv("HOME", home)

	RememberConfig("/somewhere/qubesome.config")

	_, ok := RememberedConfig()
	assert.False(t, ok)
}
