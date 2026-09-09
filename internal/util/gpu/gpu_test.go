package gpu

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/qubesome/cli/internal/files"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func notFound(string) (string, error) {
	return "", errors.New("not found")
}

func found(string) (string, error) {
	return "/usr/bin/nvidia-container-toolkit", nil
}

func TestSandboxEdits(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "dev/dri"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "dev/dri/renderD128"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "dev/dri/card0"), nil, 0o600))

	nodes, mounts, err := SandboxEdits(root)
	require.NoError(t, err)

	paths := make([]string, 0, len(nodes))
	for _, n := range nodes {
		paths = append(paths, n.Path)
	}
	assert.ElementsMatch(t, []string{"/dev/dri/renderD128", "/dev/dri/card0"}, paths)
	assert.Empty(t, mounts, "no Vulkan ICDs in the fixture")
}

func TestSandboxEditsNoGPU(t *testing.T) {
	t.Parallel()

	_, _, err := SandboxEdits(t.TempDir())
	assert.ErrorIs(t, err, ErrNoGPU)
}

func TestNvidiaToolkitPresent(t *testing.T) {
	t.Parallel()

	assert.True(t, nvidiaToolkitPresent(found))
	assert.False(t, nvidiaToolkitPresent(notFound))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), files.DirMode))
	require.NoError(t, os.WriteFile(path, []byte(content), files.FileMode))
}

// Describe used to render the arguments Params produced for a container
// runner, so it answered for a runtime qubesome no longer has. An AMD
// host was told "GPU shared with: --device=/dev/kfd", naming a flag
// nothing passes.
func TestDescribe(t *testing.T) {
	t.Parallel()

	t.Run("no gpu", func(t *testing.T) {
		t.Parallel()

		require.Equal(t, "no GPU detected", describe(t.TempDir(), notFound))
	})

	t.Run("nvidia toolkit says a sandbox cannot use it", func(t *testing.T) {
		t.Parallel()

		got := describe(t.TempDir(), found)
		require.Contains(t, got, "nvidia container toolkit")
		require.Contains(t, got, "without a GPU")
	})

	t.Run("render nodes are named, and no runner flag appears", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "dev/dri"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "dev/dri/renderD128"), nil, 0o600))

		got := describe(root, notFound)
		require.Contains(t, got, "/dev/dri/renderD128")
		require.NotContains(t, got, "--device")
		require.NotContains(t, got, "--gpus")
	})
}
