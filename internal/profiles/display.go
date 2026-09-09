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

// DisplayParams describes the display stack a profile runs: a Wayland
// compositor hosting a rootful Xwayland, with the window manager as
// Xwayland's only client.
type DisplayParams struct {
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

	// Fullscreen makes the compositor fill a host screen rather than
	// being a window the host window manager places.
	Fullscreen bool
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
func compositorArgs(p DisplayParams) ([]string, error) {
	w, h, err := splitGeometry(p.Geometry)
	if err != nil {
		return nil, err
	}

	args := []string{
		// The compositor always presents through the host X server, so
		// the profile needs nothing from the host session beyond the
		// display socket it already has. A Wayland desktop reaches it
		// through its own Xwayland, which every mainstream one runs. A
		// Wayland session without Xwayland is the case this does not
		// cover, and it would need the host compositor socket mounting
		// in and a second backend here.
		"--backend=x11",
		"--width=" + w,
		"--height=" + h,
		// The kiosk shell draws no panel or background, so the profile
		// fills the compositor window instead of being framed by a
		// second desktop.
		"--shell=kiosk-shell.so",
		"--socket=" + compositorSocket,
	}

	if p.Fullscreen {
		args = append(args, "--fullscreen")
	}

	return args, nil
}

// xwaylandArgs returns the arguments for xwayland-run, which starts a
// rootful Xwayland inside the compositor and runs the window manager as
// its only client.
//
// The window manager is passed as separate arguments rather than through a
// shell, so a window manager command from a profile's dotfiles cannot be
// made to run anything else.
func xwaylandArgs(p DisplayParams) ([]string, error) {
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
		"env", "-u", "WAYLAND_DISPLAY",
		"XDG_RUNTIME_DIR="+appRuntimeDir,
		"XAUTHORITY="+clientAuthFile)

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

// compositorEnv returns the environment the compositor runs with.
//
// XDG_RUNTIME_DIR is the profile's private directory rather than the one
// workloads share, which is what keeps the Wayland socket out of their
// reach. It is appended last because a later entry wins, so it overrides
// whatever the profile container inherited.
func compositorEnv(base []string) []string {
	return append(base, "XDG_RUNTIME_DIR="+compositorRuntimeDir)
}

// xwaylandEnv returns the environment xwayland-run runs with.
//
// It needs the same private runtime directory as the compositor, plus the
// socket name within it, so that it connects to the profile's compositor
// rather than looking for one elsewhere.
func xwaylandEnv(base []string) []string {
	return append(base,
		"XDG_RUNTIME_DIR="+compositorRuntimeDir,
		"WAYLAND_DISPLAY="+compositorSocket,
	)
}

// RunDisplay starts the profile's display stack. It is the entrypoint of
// the profile container, and it does not return until the window manager
// exits.
func RunDisplay(p DisplayParams) error {
	// The compositor's runtime dir is private to this container. It must
	// not be under /run/user/1000, which is bind-mounted into every
	// workload of this profile.
	if err := os.MkdirAll(compositorRuntimeDir, 0o700); err != nil {
		return fmt.Errorf("failed to create compositor runtime dir: %w", err)
	}
	if err := os.Chmod(compositorRuntimeDir, 0o700); err != nil {
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
	// The binary is a fixed absolute path and every argument is built by
	// compositorArgs from validated profile config, never from a caller.
	compositor := execabs.Command(files.WestonBinary, cArgs...) //nolint:gosec // G204: fixed binary, arguments built here
	compositor.Env = compositorEnv(os.Environ())
	compositor.Stdout = os.Stdout
	compositor.Stderr = os.Stderr

	if err := compositor.Start(); err != nil {
		return fmt.Errorf("failed to start compositor: %w", err)
	}

	// The compositor is reaped here rather than only after it is killed,
	// so that a compositor which dies on its own is noticed. When it goes
	// first, everything downstream reports the symptom instead: Xwayland
	// says its Wayland connection was reset, the window manager says the
	// X server connection broke, and the compositor's own exit status,
	// the one diagnostic that says why, is never looked at. The same
	// holds when the socket never appears and the wait below times out.
	//
	// The channel is buffered so this send never blocks, and every read
	// puts the status back for the next one.
	compositorExit := make(chan error, 1)
	go func() { compositorExit <- compositor.Wait() }()

	defer func() {
		if compositor.Process != nil {
			_ = compositor.Process.Kill()
		}

		err := <-compositorExit
		compositorExit <- err
		slog.Debug("compositor exited", "error", err)
	}()

	socket := filepath.Join(compositorRuntimeDir, compositorSocket)
	if err := waitForSocket(socket, 15*time.Second); err != nil {
		return errors.Join(err, compositorFailure(compositorExit))
	}

	slog.Debug("starting Xwayland", "binary", files.XwaylandRunBinary, "args", xArgs)
	// As above. xwaylandArgs builds these, and the window manager reaches
	// it as separate arguments rather than through a shell.
	x := execabs.Command(files.XwaylandRunBinary, xArgs...) //nolint:gosec // G204: fixed binary, arguments built here
	x.Env = xwaylandEnv(os.Environ())
	x.Stdin = os.Stdin
	x.Stdout = os.Stdout
	x.Stderr = os.Stderr

	// The window manager exiting on its own is how a profile stops, so
	// the compositor is only asked about when something did go wrong.
	// Asking either way would turn the race between the two shutting
	// down into an intermittent failure.
	if err := x.Run(); err != nil {
		exited, cerr := compositorStatus(compositorExit)

		// A compositor that exited reporting success was asked to shut
		// down and did, which is the profile being closed. The window
		// manager and Xwayland then lose the socket they were running
		// on and exit non-zero, but that is the shutdown arriving at
		// them rather than a failure of their own, and reporting it as
		// one turns closing a profile into an error.
		if exited && cerr == nil {
			slog.Info("compositor shut down, stopping the profile")
			return nil
		}

		return errors.Join(err, compositorFailure(compositorExit))
	}

	return nil
}

// compositorStatus reports whether the compositor has already exited and
// what it exited with. A nil error from an exited compositor is a clean
// shutdown, which is why the two are returned separately: nil alone
// cannot say whether it is still running.
//
// It reads the status without consuming it, so the deferred reap still
// finds one waiting for it.
func compositorStatus(exit chan error) (bool, error) {
	select {
	case err := <-exit:
		exit <- err
		return true, err
	default:
		return false, nil
	}
}

// compositorFailure reports how the compositor exited, if it has already
// done so, and nil while it is still running.
//
// Every caller of this is on a path where the compositor should still be
// up, so exiting at all is the failure, whatever it exited with.
func compositorFailure(exit chan error) error {
	exited, err := compositorStatus(exit)
	if !exited {
		return nil
	}

	if err == nil {
		return errors.New("compositor exited first, reporting success")
	}

	return fmt.Errorf("compositor exited first: %w", err)
}

const (
	// compositorRuntimeDir holds the profile's Wayland socket.
	//
	// It is under /tmp because the profile runs as an unprivileged user
	// with no capabilities, and /run in the image is owned by root, so
	// there is nowhere to create it there. The profile container's /tmp is
	// its own. Workloads mount host paths at /tmp/.X11-unix and
	// /tmp/qube.sock rather than the profile's /tmp itself, so nothing
	// here is reachable from a workload.
	//
	// It is deliberately not under /run/user/1000, which every workload of
	// the profile mounts.
	compositorRuntimeDir = "/tmp/qubesome-wl"

	// compositorSocket is the Wayland socket name within
	// compositorRuntimeDir.
	compositorSocket = "qubesome"

	// appRuntimeDir is the XDG_RUNTIME_DIR restored for the window manager
	// and everything it spawns, so they do not inherit a path to the
	// compositor.
	appRuntimeDir = "/run/user/1000"

	// clientAuthFile is the X cookie the window manager connects with.
	//
	// xwayland-run generates an Xauthority of its own and points the
	// client at it, while the server is left using the one qubesome
	// passes. The two hold different cookies, so the client has to be
	// pointed back at qubesome's. Workloads mount this same file, so it
	// is also what makes them able to connect at all.
	clientAuthFile = "/home/xorg-user/.Xauthority"
)
