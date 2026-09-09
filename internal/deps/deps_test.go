package deps

import (
	"os"
	"path/filepath"
	"testing"

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
