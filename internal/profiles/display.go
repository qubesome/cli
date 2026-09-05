package profiles

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"time"
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
		return "", "", fmt.Errorf("host screen resolution %q is not WIDTHxHEIGHT", geometry)
	}

	for _, v := range []string{w, h} {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return "", "", fmt.Errorf(
				"host screen resolution %q has dimension %q, which is not a positive integer",
				geometry, v)
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

// xwaylandArgs returns the arguments for xwayland-run, which starts a
// rootful Xwayland inside the compositor and runs the window manager as
// its only client.
//
// The window manager is passed as separate arguments rather than through a
// shell, so a window manager command from a profile's dotfiles cannot be
// made to run anything else.
func xwaylandArgs(p displayParams) ([]string, error) {
	if _, _, err := splitGeometry(p.Geometry); err != nil {
		return nil, err
	}

	wm := strings.Fields(strings.TrimPrefix(p.WindowManager, "exec "))
	if len(wm) == 0 {
		return nil, fmt.Errorf("profile window manager %q has no command", p.WindowManager)
	}

	args := []string{
		":" + strconv.Itoa(int(p.Display)),
		"-host-grab",
		"-geometry", p.Geometry,
		"-auth", p.AuthFile,
		// -extension disables an extension. MIT-SHM and XTEST are off so
		// that workloads sharing this display cannot pass shared memory
		// between themselves or inject synthetic input into each other.
		// RECORD is off for the same reason: it would let any client
		// record every other client's input.
		"-extension", "MIT-SHM",
		"-extension", "XTEST",
		"-extension", "RECORD",
		"-nopn",
		"-tst",
		"-nolisten", "tcp",
	}

	if p.ExtraArgs != "" {
		args = append(args, strings.Fields(p.ExtraArgs)...)
	}

	// The window manager, and everything it launches, must not inherit a
	// path to the compositor. A client that reaches the Wayland socket
	// bypasses Xwayland and the isolation set above.
	args = append(args, "--",
		"env", "-u", "WAYLAND_DISPLAY", "XDG_RUNTIME_DIR="+p.AppRuntimeDir)

	return append(args, wm...), nil
}

// waitForSocket blocks until path is a unix socket, or timeout elapses.
//
// Xwayland fails immediately if the compositor is not yet listening, so
// starting it before the socket exists turns a startup race into an
// intermittent failure.
func waitForSocket(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for {
		fi, err := os.Stat(path)
		if err == nil {
			if fi.Mode().Type()&os.ModeSocket == 0 {
				return fmt.Errorf("%q is not a socket: %s", path, fi.Mode().Type())
			}
			return nil
		}

		// Only a missing socket means it may still be on its way. Any
		// other error, a permission one for instance, would otherwise be
		// reported as a timeout much later and further from the cause.
		if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("failed to stat %q: %w", path, err)
		}

		if deadline.Before(time.Now()) {
			return fmt.Errorf("timed out waiting for compositor socket %q", path)
		}

		time.Sleep(10 * time.Millisecond)
	}
}
