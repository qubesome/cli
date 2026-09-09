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

func TestParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		files    []string
		runner   string
		lookPath func(string) (string, error)
		want     []string
		wantOK   bool
	}{
		{
			name:     "no gpu",
			runner:   "docker",
			lookPath: notFound,
			wantOK:   false,
		},
		{
			name:     "nvidia toolkit on docker",
			runner:   "docker",
			lookPath: found,
			want:     []string{"--gpus=all"},
			wantOK:   true,
		},
		{
			name:     "nvidia toolkit on podman",
			runner:   "podman",
			lookPath: found,
			want:     []string{"--device=nvidia.com/gpu=all"},
			wantOK:   true,
		},
		{
			name:     "amd kfd",
			files:    []string{"dev/kfd"},
			runner:   "docker",
			lookPath: notFound,
			want:     []string{"--device=/dev/kfd"},
			wantOK:   true,
		},
		{
			name:     "render node only",
			files:    []string{"dev/dri/renderD128"},
			runner:   "docker",
			lookPath: notFound,
			wantOK:   true,
		},
		{
			name:     "card without render node",
			files:    []string{"dev/dri/card1"},
			runner:   "docker",
			lookPath: notFound,
			wantOK:   false,
		},
		{
			name:     "cdi spec takes precedence over kfd",
			files:    []string{"dev/kfd", "dev/dri/renderD128", filepath.Join(cdiSpecDirs[0], CDISpecName)},
			runner:   "docker",
			lookPath: notFound,
			want:     []string{"--device=" + CDIKind + "=all"},
			wantOK:   true,
		},
		{
			name:     "cdi spec in var run",
			files:    []string{filepath.Join(cdiSpecDirs[1], CDISpecName)},
			runner:   "podman",
			lookPath: notFound,
			want:     []string{"--device=" + CDIKind + "=all"},
			wantOK:   true,
		},
		{
			name:     "nvidia takes precedence over cdi spec",
			files:    []string{filepath.Join(cdiSpecDirs[0], CDISpecName)},
			runner:   "docker",
			lookPath: found,
			want:     []string{"--gpus=all"},
			wantOK:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			for _, f := range tc.files {
				writeFile(t, filepath.Join(root, f), "")
			}

			got, ok := params(root, tc.runner, tc.lookPath)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.want, got)
		})
	}
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
