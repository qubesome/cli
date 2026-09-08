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

	// RuntimeDir is an XDG runtime directory created inside the sandbox,
	// or empty for none. It is created 0700 and owned by the sandbox
	// user, which is what the specification requires of XDG_RUNTIME_DIR,
	// and it exists whether or not the image ships one. Relying on the
	// image to ship it is what left dbus with no runtime directory to
	// work in.
	RuntimeDir string

	// Env is the complete environment of the sandboxed process. Container
	// runners applied the image environment implicitly and bwrap does not,
	// so the image environment belongs here too.
	Env []string

	// Args is the command to run, argv[0] first.
	Args []string

	// Cwd is the working directory of the sandboxed process. Container
	// runners took it from the image and bwrap does not, so without it
	// the sandbox inherits qubesome's own working directory, which
	// usually does not exist inside the root filesystem. Empty leaves it
	// at bwrap's default of /.
	Cwd string

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

	// CapsAdd are the capabilities the sandbox keeps, named the way bwrap
	// names them, with the CAP_ prefix. A bare NET_ADMIN is rejected as an
	// unknown capability.
	//
	// bwrap applies capability arguments in the order they appear, and
	// Args emits --cap-drop ALL in its prologue ahead of everything else,
	// so a grant rendered here lands after the drop and survives it. That
	// ordering is the whole mechanism. Moving the drop below these would
	// empty the set again and leave no trace on the command line that it
	// had.
	//
	// The grant is held in the user namespace bwrap creates, not the
	// host's. CAP_NET_ADMIN here administers the sandbox's own network
	// namespace and can do nothing to the host's.
	//
	// bwrap --help says these apply "when running as privileged user".
	// That is misleading. Measured against bubblewrap 0.11.2, an
	// unprivileged --cap-add CAP_NET_ADMIN leaves CapEff bit 12 set
	// inside the sandbox.
	CapsAdd []string

	// Seccomp applies the embedded filter. It is false for
	// seccompUnconfined.
	Seccomp bool

	// DieWithParent kills the sandbox when the process that started it
	// dies. bwrap asks the kernel for a parent death signal, so it holds
	// however the parent goes, a kill included.
	//
	// A profile sets it. The qubesome process that starts a profile also
	// serves its socket and stays up for as long as the profile runs, so
	// the two lifetimes are meant to be the same one.
	//
	// A workload leaves it off. Its launch may be a qubesome run typed at
	// a terminal, which returns as soon as the workload is up, and a
	// workload tied to that process would not outlive the shell prompt
	// coming back. The container runner detached a workload for the same
	// reason. What still ties a workload to its profile is the display:
	// the X server it draws on lives inside the profile's sandbox, so a
	// profile that goes away takes the connection with it.
	DieWithParent bool

	// Interactive keeps the sandbox attached to the terminal. It skips
	// --new-session, which calls setsid and would detach the controlling
	// terminal an interactive shell needs.
	Interactive bool

	// DisableUserns stops the sandbox creating further user namespaces.
	// Without it a process inside can unshare one, hold every capability
	// in the child and reach the mount syscalls the vendored seccomp
	// profile allows. It is a bwrap flag, so it holds even for a sandbox
	// that runs with no seccomp filter at all.
	//
	// It is off by default because a workload may legitimately need
	// nesting. Chromium's own sandbox is the case that matters, and it
	// builds a user namespace of its own. A profile runs no such thing,
	// so the profile turns it on.
	DisableUserns bool
}
