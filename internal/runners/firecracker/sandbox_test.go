package firecracker

import (
	"testing"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/sandbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func vmSandboxInput() vmSandbox {
	return vmSandbox{
		Rootfs:         "/home/coder/.qubesome/images/unpacked/sha256-gw/rootfs",
		FirecrackerBin: "/usr/bin/firecracker",
		QubesomeBin:    "/usr/local/bin/qubesome",
		AgentDir:       "/home/coder/.qubesome/run/personal/agent/dev",
		RuntimeDir:     "/home/coder/.qubesome/run/personal/vm/dev",
		Kernel:         "/home/coder/.qubesome/vmlinux",
		ConfigPath:     "/home/coder/.qubesome/run/personal/vm/dev/firecracker.cfg",
		APISocket:      "/home/coder/.qubesome/run/personal/vm/dev/api.sock",
	}
}

// The whole guard rests on this. A sandbox holding CAP_NET_ADMIN over its
// own network namespace could dissolve the bridge, renumber the tap and
// unload the nft table, so a VM escape would reach the network with
// nothing classifying it.
func TestVMSandboxHoldsNoCapabilities(t *testing.T) {
	t.Parallel()

	spec, err := buildVMSpec(vmSandboxInput())
	require.NoError(t, err)
	assert.Empty(t, spec.CapsAdd)
}

func TestVMSandboxTakesItsOwnNetworkNamespace(t *testing.T) {
	t.Parallel()

	spec, err := buildVMSpec(vmSandboxInput())
	require.NoError(t, err)
	assert.Equal(t, sandbox.NetGateway, spec.Net)
}

// kvm to run a machine at all, and tun to open the tap qubesome made.
func TestVMSandboxGetsKVMAndTun(t *testing.T) {
	t.Parallel()

	spec, err := buildVMSpec(vmSandboxInput())
	require.NoError(t, err)
	assert.Contains(t, spec.Devices, "/dev/kvm")
	assert.Contains(t, spec.Devices, "/dev/net/tun")
}

// A gated supervisor, so nothing starts until the veth, the tap and the
// guard are all in place.
func TestVMSandboxRunsAGatedSupervisor(t *testing.T) {
	t.Parallel()

	spec, err := buildVMSpec(vmSandboxInput())
	require.NoError(t, err)

	assert.Equal(t,
		[]string{files.InProfileBinary, sandbox.SuperviseCommand, sandbox.GatedFlag, "/usr/bin/firecracker"},
		spec.Args[:4])
	assert.Equal(t,
		[]string{"--api-sock", "/home/coder/.qubesome/run/personal/vm/dev/api.sock",
			"--config-file", "/home/coder/.qubesome/run/personal/vm/dev/firecracker.cfg"},
		spec.Args[4:])
}

// The machine description carries absolute host paths and is read inside
// the sandbox, so every one of them has to be bound where it already is.
// A translated description would be two spellings of each path to keep in
// step.
func TestVMSandboxBindsEveryPathTheDescriptionNames(t *testing.T) {
	t.Parallel()

	in := vmSandboxInput()
	in.DataPath = "/home/coder/vm-data/dev.ext4"

	spec, err := buildVMSpec(in)
	require.NoError(t, err)

	for _, want := range []string{in.Kernel, in.RuntimeDir, in.DataPath, in.FirecrackerBin} {
		assert.True(t, boundAt(spec, want), "%s must be bound at its own path", want)
	}
}

// The persistent disk is the one thing here that outlives the machine, so
// it is bound writable. Everything a machine writes to its root filesystem
// goes with it at shutdown.
func TestVMSandboxBindsTheDataDiskWritable(t *testing.T) {
	t.Parallel()

	in := vmSandboxInput()
	in.DataPath = "/home/coder/vm-data/dev.ext4"

	spec, err := buildVMSpec(in)
	require.NoError(t, err)

	for _, m := range spec.Mounts {
		if m.Dst == in.DataPath {
			assert.False(t, m.ReadOnly)
			return
		}
	}

	t.Fatal("the data disk was not bound")
}

// A workload that configured no persistent disk gets no bind for one,
// rather than a bind of the empty path.
func TestVMSandboxWithoutADataDiskBindsNone(t *testing.T) {
	t.Parallel()

	spec, err := buildVMSpec(vmSandboxInput())
	require.NoError(t, err)

	for _, m := range spec.Mounts {
		assert.NotEmpty(t, m.Src)
		assert.NotEmpty(t, m.Dst)
	}
}

// Nothing of the user's session reaches the VMM. The workload's own files
// are inside the machine, composed into the image it boots.
func TestVMSandboxCarriesNothingOfTheSession(t *testing.T) {
	t.Parallel()

	spec, err := buildVMSpec(vmSandboxInput())
	require.NoError(t, err)

	assert.Empty(t, spec.Env)
	assert.Empty(t, spec.RuntimeDir)
	assert.False(t, spec.DieWithParent)
}

func TestVMSandboxRefusesAnIncompleteHost(t *testing.T) {
	t.Parallel()

	for name, mutate := range map[string]func(*vmSandbox){
		"no rootfs":     func(in *vmSandbox) { in.Rootfs = "" },
		"no VMM":        func(in *vmSandbox) { in.FirecrackerBin = "" },
		"no kernel":     func(in *vmSandbox) { in.Kernel = "" },
		"no supervisor": func(in *vmSandbox) { in.QubesomeBin = "" },
		"no agent dir":  func(in *vmSandbox) { in.AgentDir = "" },
	} {
		in := vmSandboxInput()
		mutate(&in)

		_, err := buildVMSpec(in)
		require.Error(t, err, name)
	}
}

func boundAt(spec sandbox.Spec, path string) bool {
	for _, m := range spec.Mounts {
		if m.Src == path && m.Dst == path {
			return true
		}
	}

	return false
}

// A distro packaged firecracker is dynamically linked, and the gateway
// image's glibc is not the one it was built against, so the host's
// library directories come with it.
func TestVMSandboxBindsTheHostLibraries(t *testing.T) {
	t.Parallel()

	in := vmSandboxInput()
	in.LibDirs = []string{"/lib64"}

	spec, err := buildVMSpec(in)
	require.NoError(t, err)

	for _, m := range spec.Mounts {
		if m.Dst == "/lib64" {
			assert.True(t, m.ReadOnly, "the host's libraries must be read-only")
			return
		}
	}

	t.Fatal("the host's libraries were not bound")
}

// Libraries and nothing else. This is a sandbox around untrusted code, so
// a VM escape must not find the host's binaries or its data here.
func TestVMSandboxBindsNoHostTreeBeyondLibraries(t *testing.T) {
	t.Parallel()

	in := vmSandboxInput()
	in.LibDirs = []string{"/lib64"}

	spec, err := buildVMSpec(in)
	require.NoError(t, err)

	for _, m := range spec.Mounts {
		for _, forbidden := range []string{"/usr", "/bin", "/sbin", "/etc", "/home", "/var", "/"} {
			assert.NotEqual(t, forbidden, m.Dst, "a VM escape must not find %s here", forbidden)
		}
	}
}

// A statically linked firecracker needs no libraries, and that is not an
// error.
func TestVMSandboxWithoutLibrariesIsFine(t *testing.T) {
	t.Parallel()

	spec, err := buildVMSpec(vmSandboxInput())
	require.NoError(t, err)
	assert.NotEmpty(t, spec.Mounts)
}
