package deps

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qubesome/cli/internal/files"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMaxUserNamespaces(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "max_user_namespaces")

	require.NoError(t, os.WriteFile(path, []byte("253453\n"), 0o600))
	n, err := readSysctl(path)
	require.NoError(t, err)
	assert.Equal(t, 253453, n)

	require.NoError(t, os.WriteFile(path, []byte("0\n"), 0o600))
	n, err = readSysctl(path)
	require.NoError(t, err)
	assert.Zero(t, n)
}

func TestMaxUserNamespacesMissing(t *testing.T) {
	t.Parallel()

	_, err := readSysctl(filepath.Join(t.TempDir(), "missing"))
	assert.Error(t, err)
}

// A uaccess ACL is what gives the profile the render node without group
// membership. Without it the profile silently drops to software rendering,
// which is the failure this work exists to remove, so it is checked.
func TestHasUaccessACL(t *testing.T) {
	t.Parallel()

	acl := "# file: dev/dri/renderD128\n# owner: root\n# group: render\nuser::rw-\nuser:levi:rw-\ngroup::rw-\n"
	assert.True(t, hasUserACL(acl, "levi"))
	assert.False(t, hasUserACL(acl, "someone-else"))

	plain := "# file: dev/dri/renderD128\n# owner: root\n# group: render\nuser::rw-\ngroup::rw-\n"
	assert.False(t, hasUserACL(plain, "levi"))
}

// Every command that opens a profile or a workload builds it with the
// same three tools, and the table drifted apart once already: run,
// xdg-open and images asked for a container runner long after nothing
// used one.
func TestSandboxCommandsRequireTheSandboxTools(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"run", "xdg-open", "start"} {
		for _, tool := range sandboxTools {
			assert.Contains(t, deps[name], tool, name)
		}
	}

	// images fills the store and opens nothing, so it needs what fetches
	// and unpacks and not what would have launched the result.
	for _, tool := range imageTools {
		assert.Contains(t, deps["images"], tool)
	}
	assert.NotContains(t, deps["images"], files.BwrapBinary)
}

// docker is left in exactly one place: firecracker runs it to build a
// root filesystem and to set up its network taps, and it is optional
// because firecracker is. Anything else naming a container runner is a
// leftover, and a required entry would report a host as broken for
// missing a binary nothing runs.
func TestOnlyFirecrackerStillNeedsAContainerRunner(t *testing.T) {
	t.Parallel()

	for name, list := range deps {
		assert.NotContains(t, list, files.DockerBinary, name)
		assert.NotContains(t, list, files.PodmanBinary, name)
	}

	for name, list := range optionalDeps {
		assert.Contains(t, list, files.FireCrackerBinary, name)
		assert.NotContains(t, list, files.PodmanBinary, name)
	}

	assert.Contains(t, optionalDeps["run"], files.DockerBinary)
	assert.Contains(t, optionalDeps["xdg-open"], files.DockerBinary)
}
