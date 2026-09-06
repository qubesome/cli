package profiles

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qubesome/cli/internal/files"
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

func TestStartWithoutAConfig(t *testing.T) {
	t.Parallel()

	err := Start("", &types.Profile{Name: "work", WindowManager: "i3"}, nil, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config is nil")
}
