package files

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureMappedDir(t *testing.T) {
	t.Parallel()

	t.Run("creates missing dir owned by the current user", func(t *testing.T) {
		t.Parallel()

		dir := filepath.Join(t.TempDir(), "missing")
		require.NoError(t, EnsureMappedDir(dir+"/"))

		fi, err := os.Stat(dir)
		require.NoError(t, err)
		assert.True(t, fi.IsDir())
		assert.Equal(t, os.FileMode(DirMode), fi.Mode().Perm())

		st, ok := fi.Sys().(*syscall.Stat_t)
		require.True(t, ok)
		assert.Equal(t, os.Getuid(), int(st.Uid))
		assert.Equal(t, os.Getgid(), int(st.Gid))
	})

	t.Run("does not create missing dir without trailing separator", func(t *testing.T) {
		t.Parallel()

		dir := filepath.Join(t.TempDir(), "missing")
		require.ErrorIs(t, EnsureMappedDir(dir), ErrMissingMappedPath)

		_, err := os.Stat(dir)
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("does not create missing parent dirs", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		require.ErrorIs(t, EnsureMappedDir(filepath.Join(root, "a", "b")+"/"), ErrMissingMappedPath)

		_, err := os.Stat(filepath.Join(root, "a"))
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("leaves existing dir untouched", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, os.Chmod(dir, 0o755))
		require.NoError(t, EnsureMappedDir(dir))

		fi, err := os.Stat(dir)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o755), fi.Mode().Perm())
	})

	t.Run("leaves existing file untouched", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(path, []byte("data"), FileMode))
		require.NoError(t, EnsureMappedDir(path))

		fi, err := os.Stat(path)
		require.NoError(t, err)
		assert.False(t, fi.IsDir())
	})

	t.Run("reports the parent not being a dir", func(t *testing.T) {
		t.Parallel()

		parent := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(parent, []byte("data"), FileMode))

		err := EnsureMappedDir(filepath.Join(parent, "child") + "/")
		require.ErrorIs(t, err, syscall.ENOTDIR)
		assert.NotErrorIs(t, err, ErrMissingMappedPath)
	})

	t.Run("reports a trailing separator on a file", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(path, []byte("data"), FileMode))

		err := EnsureMappedDir(path + "/")
		require.ErrorIs(t, err, syscall.ENOTDIR)
		assert.NotErrorIs(t, err, ErrMissingMappedPath)
	})

	t.Run("reports permission errors as they are", func(t *testing.T) {
		t.Parallel()

		if os.Getuid() == 0 {
			t.Skip("root bypasses dir permissions")
		}

		root := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(root, "locked"), 0o000))

		err := EnsureMappedDir(filepath.Join(root, "locked", "child") + "/")
		require.ErrorIs(t, err, os.ErrPermission)
		assert.NotErrorIs(t, err, ErrMissingMappedPath)
	})
}
