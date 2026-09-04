package gpu

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func icd(libPath string) string {
	return `{"file_format_version":"1.0.0","ICD":{"library_path":"` + libPath + `","api_version":"1.4.303"}}`
}

func TestNewSpec(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "dev/dri/renderD128"), "")
	writeFile(t, filepath.Join(root, "dev/dri/card1"), "")
	writeFile(t, filepath.Join(root, "usr/share/vulkan/icd.d/radeon_icd.x86_64.json"), icd("/usr/lib64/libvulkan_radeon.so"))
	writeFile(t, filepath.Join(root, "usr/share/vulkan/icd.d/intel_icd.x86_64.json"), icd("libvulkan_intel.so"))
	writeFile(t, filepath.Join(root, "usr/share/vulkan/icd.d/lvp_icd.x86_64.json"), icd("/usr/lib64/libvulkan_lvp.so"))
	writeFile(t, filepath.Join(root, "usr/lib64/libvulkan_radeon.so"), "")
	writeFile(t, filepath.Join(root, "usr/lib64/libvulkan_lvp.so"), "")
	writeFile(t, filepath.Join(root, "usr/share/vulkan/icd.d/libvulkan_intel.so"), "")

	spec, err := NewSpec(root)
	require.NoError(t, err)

	assert.Equal(t, CDIKind, spec.Kind)
	require.Len(t, spec.Devices, 1)

	edits := spec.Devices[0].ContainerEdits
	assert.Equal(t, []DeviceNode{
		{Path: "/dev/dri/card1"},
		{Path: "/dev/dri/renderD128"},
	}, edits.DeviceNodes)

	assert.Equal(t, []Mount{
		{
			HostPath:      filepath.Join(root, "usr/share/vulkan/icd.d/intel_icd.x86_64.json"),
			ContainerPath: "/usr/share/vulkan/icd.d/intel_icd.x86_64.json",
			Options:       []string{"ro", "bind"},
		},
		{
			HostPath:      filepath.Join(root, "usr/share/vulkan/icd.d/libvulkan_intel.so"),
			ContainerPath: "/usr/share/vulkan/icd.d/libvulkan_intel.so",
			Options:       []string{"ro", "bind"},
		},
		{
			HostPath:      filepath.Join(root, "usr/share/vulkan/icd.d/radeon_icd.x86_64.json"),
			ContainerPath: "/usr/share/vulkan/icd.d/radeon_icd.x86_64.json",
			Options:       []string{"ro", "bind"},
		},
		{
			HostPath:      filepath.Join(root, "usr/lib64/libvulkan_radeon.so"),
			ContainerPath: "/usr/lib64/libvulkan_radeon.so",
			Options:       []string{"ro", "bind"},
		},
	}, edits.Mounts)
}

func TestNewSpecSkipsICDWithMissingLibrary(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "dev/dri/renderD128"), "")
	writeFile(t, filepath.Join(root, "usr/share/vulkan/icd.d/radeon_icd.x86_64.json"), icd("/usr/lib64/gone.so"))

	spec, err := NewSpec(root)
	require.NoError(t, err)
	assert.Empty(t, spec.Devices[0].ContainerEdits.Mounts)
}

func TestNewSpecWithoutRenderNode(t *testing.T) {
	t.Parallel()

	_, err := NewSpec(t.TempDir())
	require.ErrorIs(t, err, ErrNoGPU)
}
