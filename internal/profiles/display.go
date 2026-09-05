package profiles

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/qubesome/cli/internal/files"
	"golang.org/x/sys/execabs"
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

// RunDisplay starts the profile's display stack. It is the entrypoint of
// the profile container, and it does not return until the window manager
// exits.
func RunDisplay(p displayParams) error {
	// The compositor's runtime dir is private to this container. It must
	// not be under /run/user/1000, which is bind-mounted into every
	// workload of this profile.
	if err := os.MkdirAll(p.RuntimeDir, 0o700); err != nil {
		return fmt.Errorf("failed to create compositor runtime dir: %w", err)
	}
	if err := os.Chmod(p.RuntimeDir, 0o700); err != nil {
		return fmt.Errorf("failed to set compositor runtime dir mode: %w", err)
	}

	cArgs, err := compositorArgs(p)
	if err != nil {
		return err
	}

	xArgs, err := xwaylandArgs(p)
	if err != nil {
		return err
	}

	slog.Debug("starting compositor", "binary", files.WestonBinary, "args", cArgs)
	compositor := execabs.Command(files.WestonBinary, cArgs...)
	compositor.Env = append(os.Environ(), "XDG_RUNTIME_DIR="+p.RuntimeDir)
	compositor.Stdout = os.Stdout
	compositor.Stderr = os.Stderr

	if err := compositor.Start(); err != nil {
		return fmt.Errorf("failed to start compositor: %w", err)
	}

	defer func() {
		if compositor.Process != nil {
			_ = compositor.Process.Kill()

			// When the socket never appeared, the compositor usually died
			// during startup rather than being slow, and its exit status
			// is the diagnostic that says which. Discarding it leaves
			// only the timeout, which describes the symptom.
			state, err := compositor.Process.Wait()
			switch {
			case err != nil:
				slog.Debug("failed to reap compositor", "error", err)
			case state != nil:
				slog.Debug("compositor exited", "state", state.String())
			}
		}
	}()

	socket := filepath.Join(p.RuntimeDir, p.WaylandSocket)
	if err := waitForSocket(socket, 15*time.Second); err != nil {
		return err
	}

	slog.Debug("starting Xwayland", "binary", files.XwaylandRunBinary, "args", xArgs)
	x := execabs.Command(files.XwaylandRunBinary, xArgs...)
	x.Env = append(os.Environ(),
		"XDG_RUNTIME_DIR="+p.RuntimeDir,
		"WAYLAND_DISPLAY="+p.WaylandSocket,
	)
	x.Stdin = os.Stdin
	x.Stdout = os.Stdout
	x.Stderr = os.Stderr

	return x.Run()
}

// DisplayOptions are the profile-display command's inputs. They mirror
// displayParams, minus the fields that are fixed for every profile.
type DisplayOptions struct {
	Display       uint8
	Geometry      string
	AuthFile      string
	WindowManager string
	ExtraArgs     string
	HostWayland   bool
}

const (
	// compositorRuntimeDir holds the profile's Wayland socket. It is
	// deliberately not under /run/user/1000, which workloads share with
	// the profile.
	compositorRuntimeDir = "/run/qubesome-wl"

	// compositorSocket is the Wayland socket name within
	// compositorRuntimeDir.
	compositorSocket = "qubesome"

	// appRuntimeDir is the XDG_RUNTIME_DIR applications in the profile
	// see, restored for the window manager and its children.
	appRuntimeDir = "/run/user/1000"
)

// RunDisplayWithOptions starts the profile display stack from the values
// the profile-display command was given.
func RunDisplayWithOptions(o DisplayOptions) error {
	return RunDisplay(displayParams{
		Display:       o.Display,
		Geometry:      o.Geometry,
		AuthFile:      o.AuthFile,
		WindowManager: o.WindowManager,
		ExtraArgs:     o.ExtraArgs,
		HostWayland:   o.HostWayland,
		RuntimeDir:    compositorRuntimeDir,
		WaylandSocket: compositorSocket,
		AppRuntimeDir: appRuntimeDir,
	})
}
