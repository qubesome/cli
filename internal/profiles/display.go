package profiles

import (
	"fmt"
	"strconv"
	"strings"
)

// displayParams describes the display stack a profile runs: a Wayland
// compositor hosting a rootful Xwayland, with the window manager as
// Xwayland's only client.
type displayParams struct {
	// Display is the X display number workloads are pointed at.
	Display uint8

	// Geometry is the screen size, as WIDTHxHEIGHT.
	Geometry string

	// AuthFile is the X server cookie file, inside the profile container.
	AuthFile string

	// WindowManager is the command Xwayland runs as its client.
	WindowManager string

	// ExtraArgs are additional Xwayland arguments from profile config.
	ExtraArgs string

	// HostWayland selects the compositor backend. The compositor presents
	// to the host session either way, as a Wayland surface when true and
	// as an X11 window when false.
	HostWayland bool

	// RuntimeDir is the compositor's XDG_RUNTIME_DIR. It holds the Wayland
	// socket and must not be reachable by workloads, so it is a directory
	// private to the profile container rather than the /run/user/1000
	// that workloads share with the profile.
	RuntimeDir string

	// WaylandSocket is the compositor socket name within RuntimeDir.
	WaylandSocket string

	// AppRuntimeDir is the XDG_RUNTIME_DIR restored for the window manager
	// and everything it spawns, so they do not inherit a path to the
	// compositor.
	AppRuntimeDir string
}

// splitGeometry splits a WIDTHxHEIGHT string into its two parts. Both are
// returned as strings because they are only ever passed on as arguments,
// but they are parsed as integers so a malformed value is rejected here
// rather than by the compositor.
func splitGeometry(geometry string) (string, string, error) {
	w, h, ok := strings.Cut(geometry, "x")
	if !ok {
		return "", "", fmt.Errorf("geometry %q is not WIDTHxHEIGHT", geometry)
	}

	for _, v := range []string{w, h} {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return "", "", fmt.Errorf("geometry %q is not WIDTHxHEIGHT", geometry)
		}
	}

	return w, h, nil
}

// compositorArgs returns the arguments for the Wayland compositor that
// hosts the profile's Xwayland.
func compositorArgs(p displayParams) ([]string, error) {
	w, h, err := splitGeometry(p.Geometry)
	if err != nil {
		return nil, err
	}

	backend := "x11"
	if p.HostWayland {
		backend = "wayland"
	}

	return []string{
		"--backend=" + backend,
		"--width=" + w,
		"--height=" + h,
		// The kiosk shell draws no panel or background, so the profile
		// fills the compositor window instead of being framed by a
		// second desktop.
		"--shell=kiosk-shell.so",
		"--socket=" + p.WaylandSocket,
	}, nil
}
