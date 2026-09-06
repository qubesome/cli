package profiles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/images"
	"github.com/qubesome/cli/internal/sandbox"
	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	require.NoError(t, sandbox.WriteState(sandboxStatePath(name), os.Getpid()))

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
