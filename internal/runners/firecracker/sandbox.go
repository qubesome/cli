package firecracker

import (
	"errors"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/sandbox"
)

const (
	// kvmDevice is what a VMM needs to run a machine at all.
	kvmDevice = "/dev/kvm"

	// tunDevice is the node firecracker opens the tap through.
	//
	// The tap itself was created from outside and handed over by uid, so
	// this node is the whole of what the sandbox needs and it holds no
	// capability to go with it. See buildVMSpec.
	tunDevice = "/dev/net/tun"
)

// vmSandbox is everything the VMM's sandbox needs that comes from the
// host.
//
// It exists so that the host lookups all happen in Run and buildVMSpec
// stays a pure function of its arguments, which is the arrangement
// bwrap.input describes for the other runner.
type vmSandbox struct {
	// Rootfs is the tree the VMM runs in. It is the gateway image, which
	// is already unpacked because the gateway is running, and which is a
	// small tree with nothing of the user's session in it. It is not the
	// workload's own image: that one is the guest's root filesystem and is
	// inside the machine.
	Rootfs string

	// FirecrackerBin is the VMM on the host, bound in at the same path so
	// the sandbox can run it. The gateway image does not ship one.
	FirecrackerBin string

	// LibDirs are the host's library directories, bound read-only because
	// a distro packaged firecracker is dynamically linked and the gateway
	// image's own glibc is not the one it was built against.
	//
	// It is the library directories and nothing else. Not /usr, not /bin:
	// this is a sandbox around untrusted code rather than a host tool
	// being given a uid map, so rootfsArgs' reasoning about binding the
	// host's whole tree does not carry here. On x86-64 the ELF
	// interpreter is always /lib64/ld-linux-x86-64.so.2, so /lib64 has to
	// exist for any dynamic binary to run at all and it is where the
	// loader then finds the rest. That makes this both necessary and
	// sufficient, and it carries no binaries and no data.
	//
	// A statically linked firecracker needs none of it, and an empty list
	// is not an error for that reason.
	LibDirs []string

	// QubesomeBin is the host binary. It is the sandbox's entrypoint, as a
	// gated supervisor, and it is what starts firecracker once the gate
	// opens.
	QubesomeBin string

	// AgentDir is the supervisor's socket directory on the host, bound in
	// so that the gate can be opened from outside.
	AgentDir string

	// RuntimeDir holds the machine description, the root filesystem built
	// for this boot, the API socket and the vsock directory. All four are
	// the VMM's own, so it gets the directory rather than each of them.
	//
	// The separation files.VMAPISocket describes is about a console's
	// sandbox, which is given the vsock directory and nothing else. This
	// sandbox is the machine.
	RuntimeDir string

	// Kernel is the pinned guest kernel, bound read-only at the same path
	// the machine description names.
	Kernel string

	// DataPath is the workload's persistent disk, or empty when it
	// configured none. It is bound writable at the path the machine
	// description names, and it is the one thing here that outlives the
	// machine.
	DataPath string

	// ConfigPath and APISocket are what firecracker is told on its command
	// line. Both are under RuntimeDir.
	ConfigPath string
	APISocket  string
}

// buildVMSpec renders the sandbox the VMM runs in.
//
// It is much closer to a gateway helper than to a workload sandbox.
// Nothing of the user's session is in it: no X server, no dbus, no mapped
// paths, no keyring. The workload's own files are inside the machine,
// composed into the image BuildRootfs wrote, and what is out here is only
// what firecracker itself opens.
//
// Every host path is bound at the path it already has. The machine
// description carries absolute paths and is read inside the sandbox, so
// binding them anywhere else would mean translating the description and
// keeping two spellings of each path in step.
//
// It holds no capabilities, and that is the decision the guard rests on.
// CAP_NET_ADMIN over its own network namespace would let a process that
// escaped the machine dissolve the bridge, renumber the tap and unload the
// nft table that pins the guest's address. The tap is created from outside
// and handed over by uid instead, which needs no capability to open.
// Measured as checks 9 and 10 of hack/verify-sandbox-reentry.sh.
func buildVMSpec(in vmSandbox) (sandbox.Spec, error) {
	if in.Rootfs == "" {
		return sandbox.Spec{}, errors.New("firecracker: the microVM sandbox has no rootfs")
	}
	if in.FirecrackerBin == "" || in.Kernel == "" {
		return sandbox.Spec{}, errors.New("firecracker: the microVM sandbox has no VMM or no kernel")
	}
	if in.QubesomeBin == "" || in.AgentDir == "" {
		// Without both there is nothing to hold the VMM back while the
		// tap is made, and firecracker would start against a device that
		// is not there yet and make an unenslaved one of its own.
		return sandbox.Spec{}, errors.New("firecracker: the microVM sandbox has no supervisor binary or socket dir")
	}

	mounts := []sandbox.Mount{
		{Src: in.QubesomeBin, Dst: files.InProfileBinary, ReadOnly: true},
		{Src: in.FirecrackerBin, Dst: in.FirecrackerBin, ReadOnly: true},
		{Src: in.Kernel, Dst: in.Kernel, ReadOnly: true},
		{Src: in.AgentDir, Dst: files.InWorkloadAgentDir()},
		{Src: in.RuntimeDir, Dst: in.RuntimeDir},
	}

	for _, dir := range in.LibDirs {
		mounts = append(mounts, sandbox.Mount{Src: dir, Dst: dir, ReadOnly: true})
	}

	if in.DataPath != "" {
		mounts = append(mounts, sandbox.Mount{Src: in.DataPath, Dst: in.DataPath})
	}

	return sandbox.Spec{
		Rootfs: in.Rootfs,

		// Zero is root inside the sandbox's own user namespace, which is
		// not host root: --cap-drop ALL still empties its bounding set. It
		// is also the uid the tap was handed to, which is what lets
		// firecracker open it. See gateway.vmTapOwner.
		UID: 0,
		GID: 0,

		// Its own empty namespace, which qubesome then wires. NetGateway
		// and NetNone hand bwrap the same thing, and the difference is
		// what happens next.
		Net: sandbox.NetGateway,

		// The embedded filter is shaped for a workload rather than for a
		// VMM, and the untrusted thing here is the guest rather than
		// firecracker. Firecracker installs a filter of its own over its
		// vcpu threads, which is the one that matters for a machine.
		Seccomp: false,

		Devices: []string{kvmDevice, tunDevice},
		Mounts:  mounts,

		Args: []string{
			files.InProfileBinary, sandbox.SuperviseCommand, sandbox.GatedFlag,
			in.FirecrackerBin,
			"--api-sock", in.APISocket,
			"--config-file", in.ConfigPath,
		},
	}, nil
}
