package firecracker

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Nothing here boots. The development container has no firecracker, no
// guest kernel and no KVM, so what a machine does once it is started
// cannot be checked at all and Task 10 covers it on the target host.
// What can be checked is everything decided before the process is
// started: the machine description that is handed to it, the kernel
// command line inside that description, and the refusals the attach path
// makes, which live in internal/qubesome.

func params() configParams {
	return configParams{
		KernelImagePath: "/home/coder/.qubesome/vmlinux",
		BootArgs:        bootArgs,
		RootFsPath:      "/home/coder/.qubesome/run/personal/vm/dev/rootfs.ext4",
		VCPUs:           2,
		MemoryMiB:       1024,
		GuestCID:        guestCID,
		VsockPath:       "/home/coder/.qubesome/run/personal/vm/dev/vsock/vsock.sock",
	}
}

func TestRenderConfigBare(t *testing.T) {
	t.Parallel()

	got, err := renderConfig(params())
	require.NoError(t, err)

	goldenText(t, "machine-bare", got)
}

func TestRenderConfigWithADataDisk(t *testing.T) {
	t.Parallel()

	p := params()
	p.DataPath = "/home/coder/.local/share/qubesome/dev.ext4"
	p.VCPUs = 4
	p.MemoryMiB = 4096

	got, err := renderConfig(p)
	require.NoError(t, err)

	goldenText(t, "machine-data", got)
}

// The golden files record what the machine description looks like, and a
// regeneration would take a broken one without complaint, so what
// firecracker actually has to be able to read is asserted as well.
func TestRenderConfigIsJSON(t *testing.T) {
	t.Parallel()

	for _, data := range []string{"", "/disks/dev.ext4"} {
		p := params()
		p.DataPath = data

		got, err := renderConfig(p)
		require.NoError(t, err)

		var machine map[string]any
		require.NoError(t, json.Unmarshal([]byte(got), &machine))

		drives, ok := machine["drives"].([]any)
		require.True(t, ok)

		if data == "" {
			assert.Len(t, drives, 1, "a machine without a data disk has only its rootfs")
		} else {
			assert.Len(t, drives, 2)
		}
	}
}

// A path is configuration, and microvmPathRegex allows characters JSON
// cannot carry as they are.
func TestRenderConfigQuotesAPath(t *testing.T) {
	t.Parallel()

	p := params()
	p.DataPath = `/disks/a "quoted"\path.ext4`

	got, err := renderConfig(p)
	require.NoError(t, err)

	var machine map[string]any
	require.NoError(t, json.Unmarshal([]byte(got), &machine))

	drives, ok := machine["drives"].([]any)
	require.True(t, ok)
	require.Len(t, drives, 2)

	data, ok := drives[1].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, p.DataPath, data["path_on_host"])
}

// The kernel command line is the agreement between this runner and the
// guest init, and every word of it is load-bearing. See bootArgs.
func TestBootArgs(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"root=/dev/vda",
		"rw",
		"init=/sbin/qubesome-init",
		"vm-init",
		// sandbox.shutdown depends on this pair. Dropping reboot=k turns
		// a clean shutdown into a machine that never comes down.
		"reboot=k",
		"panic=1",
		"pci=off",
	} {
		assert.Contains(t, strings.Fields(bootArgs), want)
	}
}

func TestStatePath(t *testing.T) {
	t.Parallel()

	path, err := StatePath(types.EffectiveWorkload{
		Name:    "dev-personal",
		Profile: &types.Profile{Name: "personal"},
	})
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(path, "/personal/sandbox-dev-personal.json"), path)
}

func TestStatePathRefusals(t *testing.T) {
	t.Parallel()

	_, err := StatePath(types.EffectiveWorkload{Name: "dev-personal"})
	require.Error(t, err, "a workload with no profile has nowhere to be recorded")

	_, err = StatePath(types.EffectiveWorkload{
		Name:    "../escape",
		Profile: &types.Profile{Name: "personal"},
	})
	require.Error(t, err)
}
