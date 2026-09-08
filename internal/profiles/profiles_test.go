package profiles

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/images"
	"github.com/qubesome/cli/internal/sandbox"
	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/execabs"
)

// Start is reached directly by the local path, which never went through
// the check StartFromGit had. A second start there truncated the running
// profile's X cookies and, on its way out, deleted the profile dir the
// running one is still using.
//
// HOME is what files resolves every path from, so this test cannot run in
// parallel.
func TestStartRefusesARunningProfile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const name = "work"

	dir := files.ProfileDir(name)
	require.NoError(t, os.MkdirAll(dir, files.DirMode))

	cookie := filepath.Join(dir, ".Xserver-cookie")
	require.NoError(t, os.WriteFile(cookie, []byte("cookie"), files.FileMode))

	require.NoError(t, sandbox.WriteState(SandboxStatePath(name), os.Getpid()))

	err := Start("", &types.Profile{Name: name, WindowManager: "i3"}, &types.Config{}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already started")

	data, err := os.ReadFile(cookie)
	require.NoError(t, err, "the running profile's cookie must survive a refused start")
	assert.Equal(t, "cookie", string(data))
}

// bwrap sets neither HOME nor USER, and the image environment only
// carries what its build happened to leave behind. The X cookies are
// mounted into profileHome, so the environment has to name that home
// whatever the image says. bwrap applies --setenv in order, so the entry
// that wins is the last one.
//
// DISPLAY is read from the environment, so this test cannot run in
// parallel.
func TestSandboxEnvNamesTheProfileUser(t *testing.T) {
	t.Setenv("DISPLAY", ":0")

	bundle := images.Bundle{Env: []string{"PATH=/usr/bin", "HOME=/root"}}
	senv := sandboxEnv(bundle, []byte("ca"), []byte("cert"), []byte("key"))

	assert.Equal(t, []string{"PATH=/usr/bin", "HOME=/root"}, senv[:2],
		"the image environment must come first")

	assert.Equal(t, "/home/xorg-user", lastEnv(senv, "HOME"))
	assert.Equal(t, "xorg-user", lastEnv(senv, "USER"))
	assert.Equal(t, ":0", lastEnv(senv, "DISPLAY"))
	assert.Equal(t, "key", lastEnv(senv, "Q_MTLS_KEY"))
}

// An image that sets nothing at all still gets a home.
func TestSandboxEnvWithAnEmptyImageEnvironment(t *testing.T) {
	t.Setenv("DISPLAY", ":0")

	senv := sandboxEnv(images.Bundle{}, nil, nil, nil)

	assert.Equal(t, "/home/xorg-user", lastEnv(senv, "HOME"))
	assert.Equal(t, "xorg-user", lastEnv(senv, "USER"))
}

// lastEnv returns the value of the last assignment to name, which is the
// one bwrap keeps.
func lastEnv(env []string, name string) string {
	value := ""
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, name+"="); ok {
			value = v
		}
	}
	return value
}

func TestStartWithoutAConfig(t *testing.T) {
	t.Parallel()

	err := Start("", &types.Profile{Name: "work", WindowManager: "i3"}, nil, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config is nil")
}

// A sandbox that exits non-zero is a profile that failed. Reporting it
// through a log alone had the start return nil, so a profile whose
// compositor died on startup looked like success to its caller and left
// the shell with a zero status.
func TestAwaitSandboxReportsANonZeroExit(t *testing.T) {
	t.Parallel()

	cmd := execabs.Command(files.ShBinary, "-c", "exit 3") //nolint:gosec // G204: a fixed binary and a fixed argument, both written here.
	require.NoError(t, cmd.Start())

	err := awaitSandbox("work", cmd)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `profile "work"`)

	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit)
	assert.Equal(t, 3, exit.ExitCode())
}

func TestAwaitSandboxAcceptsACleanExit(t *testing.T) {
	t.Parallel()

	cmd := execabs.Command(files.ShBinary, "-c", "exit 0") //nolint:gosec // G204: a fixed binary and a fixed argument, both written here.
	require.NoError(t, cmd.Start())

	require.NoError(t, awaitSandbox("work", cmd))
}

func TestLoadConfigUnder(t *testing.T) {
	t.Parallel()

	const config = "profiles:\n  work:\n    path: work\n    windowManager: exec awesome\n"

	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	outside := filepath.Join(tmp, "outside")

	require.NoError(t, os.MkdirAll(filepath.Join(repo, "sub"), 0o700))
	require.NoError(t, os.MkdirAll(outside, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "qubesome.config"), []byte(config), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "sub", "qubesome.config"), []byte(config), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "qubesome.config"), []byte(config), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(repo, "escape")))

	t.Run("reads a config below the dir", func(t *testing.T) {
		t.Parallel()

		for _, path := range []string{"", "sub"} {
			rel := filepath.Join(path, "qubesome.config")
			cfg, err := loadConfigUnder(repo, rel)
			require.NoError(t, err)
			require.Equal(t, filepath.Join(repo, path), cfg.RootDir)
		}
	})

	tests := []struct {
		name string
		path string
	}{
		{name: "traversal", path: "../outside"},
		{name: "absolute", path: "/etc"},
		{name: "through a symlink out of the dir", path: "escape"},
	}

	for _, tc := range tests {
		t.Run("refuses a path with a "+tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := loadConfigUnder(repo, filepath.Join(tc.path, "qubesome.config"))
			require.Error(t, err)
		})
	}
}
