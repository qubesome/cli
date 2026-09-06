// Package sandbox renders a sandbox description into bwrap arguments.
//
// It owns no namespace code. Namespaces, uid mapping, bind mounts and
// capability dropping are all bwrap's job, and what stays here is policy:
// which mounts, which devices, which network.
package sandbox

// NetMode selects the network namespace of a sandbox.
type NetMode int

const (
	// NetNone gives the sandbox its own empty network namespace, with
	// loopback only. It is the zero value so a Spec left unset gets no
	// network rather than accidentally sharing the host's.
	NetNone NetMode = iota

	// NetHost leaves the sandbox in the host network namespace.
	NetHost
)

// Mount is a bind mount from the host into the sandbox.
type Mount struct {
	Src      string
	Dst      string
	ReadOnly bool
}

// Spec describes a sandbox. Rendering it to arguments is Args.
type Spec struct {
	// Rootfs is the read-only lower layer of the sandbox root. Writes go
	// to a tmpfs overlay and are discarded when the sandbox exits.
	Rootfs string

	Mounts []Mount

	// Devices are host device nodes shared with the sandbox.
	Devices []string

	// Env is the complete environment of the sandboxed process. Container
	// runners applied the image environment implicitly and bwrap does not,
	// so the image environment belongs here too.
	Env []string

	// Args is the command to run, argv[0] first.
	Args []string

	// UID and GID are the credentials inside the sandbox's user
	// namespace. The zero value maps to root within that namespace, which
	// is not host root: --cap-drop ALL still empties its bounding
	// capability set. Many OCI images run as root by default, so a caller
	// that leaves these unset is not a mistake to guard against here,
	// it is Task 12's job to reject it if a profile requires otherwise.
	UID int
	GID int

	Hostname string

	Net NetMode

	// Seccomp applies the embedded filter. It is false for
	// seccompUnconfined.
	Seccomp bool

	// Interactive keeps the sandbox attached to the terminal. It skips
	// --new-session, which calls setsid and would detach the controlling
	// terminal an interactive shell needs.
	Interactive bool
}
