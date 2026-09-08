package files

const (
	ShBinary          = "/bin/sh"
	XclipBinary       = "/usr/bin/xclip"
	FireCrackerBinary = "/usr/bin/firecracker"
	XrandrBinary      = "/usr/bin/xrandr"
	WlrRandrBinary    = "/usr/bin/wlr-randr"
	SetxkbmapBinary   = "/usr/bin/setxkbmap"
	LocalectlBinary   = "/usr/bin/localectl"

	// WestonBinary is the Wayland compositor that hosts the profile's
	// Xwayland. It runs inside the profile container, not on the host.
	WestonBinary = "/usr/bin/weston"

	// XwaylandRunBinary starts a rootful Xwayland inside an existing
	// Wayland session and runs one X client in it. It runs inside the
	// profile container, not on the host.
	XwaylandRunBinary = "/usr/bin/xwayland-run"

	// InProfileBinary is where the qubesome binary is bind-mounted inside
	// the profile container.
	InProfileBinary = "/usr/local/bin/qubesome"

	// BwrapBinary creates the profile sandbox. Like the container runner
	// it enforces every isolation setting qubesome asks for, so it is not
	// resolved through PATH.
	BwrapBinary = "/usr/bin/bwrap"

	// SkopeoBinary pulls images, verifying manifest and blob digests.
	SkopeoBinary = "/usr/bin/skopeo"

	// UmociBinary applies image layers, rootless.
	UmociBinary = "/usr/bin/umoci"

	// MkfsExt4Binary builds the root filesystem a microVM boots.
	//
	// It is an absolute path like every other binary here, and for one
	// reason of its own as well: /usr/sbin is frequently not on an
	// ordinary user's PATH, so exec.LookPath would report a tool that is
	// installed as missing.
	MkfsExt4Binary = "/usr/sbin/mkfs.ext4"

	// GetfaclBinary reports the ACLs on a device node. deps uses it to
	// check for the uaccess ACL that systemd-logind grants the seat's
	// user, without which a profile silently drops to software rendering.
	GetfaclBinary = "/usr/bin/getfacl"
)
