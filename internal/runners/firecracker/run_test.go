package firecracker

import (
	"encoding/json"
	"errors"
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

// A machine on the gateway is given one interface, naming the tap
// qubesome created for it and the MAC its guard pins.
func TestRenderConfigOnTheGateway(t *testing.T) {
	t.Parallel()

	p := params()
	p.TapDevice = "tap0"
	p.GuestMAC = "02:00:0a:6f:00:02"

	got, err := renderConfig(p)
	require.NoError(t, err)
	require.True(t, json.Valid([]byte(got)), got)

	assert.Contains(t, got, `"host_dev_name": "tap0"`)
	assert.Contains(t, got, `"guest_mac": "02:00:0a:6f:00:02"`)

	goldenText(t, "machine-gateway", got)
}

// A machine with no gateway gets no interface at all, rather than one
// naming a tap that was never made. That is what a machine did before
// there was a gateway to put one on, and it is kept exactly.
func TestRenderConfigWithoutAGatewayHasNoInterface(t *testing.T) {
	t.Parallel()

	got, err := renderConfig(params())
	require.NoError(t, err)
	require.True(t, json.Valid([]byte(got)), got)

	assert.NotContains(t, got, "host_dev_name")
	assert.NotContains(t, got, "guest_mac")
}

// The order is the point of it. The sandbox exists, so the wire, the tap
// and the guard are built from outside it. The gateway is told whose
// address it is, which it refuses if it has no policy for the name. Only
// then does the VMM start.
func TestVMAttachOrder(t *testing.T) {
	t.Parallel()

	att := &fakeVMAttacher{}

	require.NoError(t, vmAttach(1234, att, "dev-personal", func() error {
		att.calls = append(att.calls, "release")

		return nil
	}))

	assert.Equal(t, []string{"wire", "register", "release"}, att.calls)
}

// Fail closed. Nothing has booted when this fails, because a gated
// supervisor is still holding the VMM, and what must not happen is a
// guest reaching the network with nothing classifying it.
func TestVMAttachStopsBeforeTheVMMWhenTheGatewayRefuses(t *testing.T) {
	t.Parallel()

	var released bool
	att := &fakeVMAttacher{registerErr: errors.New("no policy for dev-personal")}

	err := vmAttach(1234, att, "dev-personal", func() error {
		released = true

		return nil
	})

	require.Error(t, err)
	assert.False(t, released, "the VMM must not start unpoliced")
	assert.Equal(t, []string{"wire", "register"}, att.calls)
}

// A wire that cannot be built is the same class of failure, and it stops
// the launch before the gateway is told anything at all.
func TestVMAttachStopsWhenTheWireFails(t *testing.T) {
	t.Parallel()

	var released bool
	att := &fakeVMAttacher{wireErr: errors.New("no tap")}

	err := vmAttach(1234, att, "dev-personal", func() error {
		released = true

		return nil
	})

	require.Error(t, err)
	assert.False(t, released)
	assert.Equal(t, []string{"wire"}, att.calls)
}

// The pid the wiring is given is the sandbox's own, as bwrap reported it,
// and not the outer process's.
func TestVMAttachWiresTheSandboxPID(t *testing.T) {
	t.Parallel()

	att := &fakeVMAttacher{}
	require.NoError(t, vmAttach(4242, att, "dev-personal", func() error { return nil }))

	assert.Equal(t, 4242, att.pid)
}

type fakeVMAttacher struct {
	calls       []string
	pid         int
	wireErr     error
	registerErr error
}

func (f *fakeVMAttacher) WireVM(pid int) error {
	f.calls = append(f.calls, "wire")
	f.pid = pid

	return f.wireErr
}

func (f *fakeVMAttacher) Register(string) error {
	f.calls = append(f.calls, "register")

	return f.registerErr
}
