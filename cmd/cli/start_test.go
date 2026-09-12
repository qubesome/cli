package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/profiles"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The child of a detached start runs the same command line again, so
// with nothing to stop it, it detaches again, and so does its child.
// Filtering the flag out of the argument list is what used to stop it,
// and it knew "-d" and "-detach" and not "--detach", which urfave/cli
// accepts as well. This is the check that the child stops.
func TestTheChildOfADetachedStartDoesNotDetachAgain(t *testing.T) {
	t.Parallel()

	background, err := detachNow(true, false, false, true)

	require.NoError(t, err)
	assert.False(t, background, "the child must do the work, not detach again")
}

func TestADetachedStartDetaches(t *testing.T) {
	t.Parallel()

	background, err := detachNow(true, false, false, false)

	require.NoError(t, err)
	assert.True(t, background)
}

func TestAStartWithoutDetachStaysInTheForeground(t *testing.T) {
	t.Parallel()

	background, err := detachNow(false, false, false, false)

	require.NoError(t, err)
	assert.False(t, background)
}

// Both were silently ignored, which left the flag doing nothing and
// saying nothing about it.
func TestDetachIsRefusedAlongsideDebugAndInteractive(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name               string
		debug, interactive bool
	}{
		{"debug", true, false},
		{"interactive", false, true},
		{"both", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := detachNow(true, tc.debug, tc.interactive, false)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "--detach cannot be used")
		})
	}
}

// A start with nowhere to look reads qubesome.config out of the working
// directory, so one run from anywhere else failed with a bare "no such
// file or directory" naming a relative path.
func TestAStartWithNowhereToLookNamesTheRememberedConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(t.TempDir())

	dir := t.TempDir()
	remembered := filepath.Join(dir, profiles.ConfigName)
	require.NoError(t, os.WriteFile(remembered, []byte("{}"), 0o600))
	files.RememberConfig(remembered)

	err := locatable("", "", "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), remembered)
	assert.Contains(t, err.Error(), "-path "+dir, "the message has to be a command to copy")
}

// With nothing remembered there is only the general answer.
func TestAStartWithNowhereToLookAndNothingRememberedSaysWhichFlags(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())

	err := locatable("", "", "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "-git, -local or -path")
}

// Anything that says where the config is settles it, and the record is
// never consulted.
func TestAStartThatWasToldWhereToLookIsLeftAlone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())

	assert.NoError(t, locatable("https://example.com/dotfiles", "", ""))
	assert.NoError(t, locatable("", "/somewhere", ""))
	assert.NoError(t, locatable("", "", "/somewhere"))
}

// A config in the working directory is what a bare start has always
// used, and it keeps using it.
func TestAStartStandingOnAConfigIsLeftAlone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, profiles.ConfigName), []byte("{}"), 0o600))
	t.Chdir(dir)

	assert.NoError(t, locatable("", "", ""))
}
