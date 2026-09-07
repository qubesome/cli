package doctor

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/qubesome/cli/internal/files"
)

// Environment checks the host qubesome runs on, independently of any
// profile or workload.
func Environment(env Env, runner string) []Check {
	return []Check{
		checkRunner(env, runner),
		checkResolution(env),
		checkHostDisplay(env),
		checkRenderNode(env),
		checkDbus(env),
		checkQubesomeDir(env),
	}
}

// checkRunner verifies that the container runner is not just installed
// but usable, since a runner that refuses every connection is no better
// than one that is missing.
func checkRunner(env Env, runner string) Check {
	bin := files.ContainerRunnerBinary(runner)

	if _, err := env.LookPath(bin); err != nil {
		return Check{
			Name:   "container runner",
			Status: Fail,
			Detail: fmt.Sprintf("%s was not found", bin),
			Fix:    "Install docker or podman, then run doctor again.",
		}
	}

	out, err := env.Output(bin, "info")
	if err == nil {
		return Check{
			Name:   "container runner",
			Status: OK,
			Detail: fmt.Sprintf("%s is installed and reachable", bin),
		}
	}

	text := string(out)
	lower := strings.ToLower(text)

	switch {
	case strings.Contains(lower, "permission denied"):
		return Check{
			Name:   "container runner",
			Status: Fail,
			Detail: fmt.Sprintf("%s refused the connection", bin),
			Fix:    "Add your user to the docker group with `sudo usermod -aG docker $USER`, then log out and back in. Alternatively, switch to podman, which does not require group membership.",
		}
	case strings.Contains(lower, "cannot connect"), strings.Contains(lower, "daemon"), strings.Contains(lower, "refused"):
		return Check{
			Name:   "container runner",
			Status: Fail,
			Detail: fmt.Sprintf("%s daemon is not reachable", bin),
			Fix:    "Start the daemon with `systemctl --user start docker` or `systemctl start docker`, depending on how it is installed.",
		}
	default:
		return Check{
			Name:   "container runner",
			Status: Fail,
			Detail: firstLine(text),
			Fix:    fmt.Sprintf("Run `%s info` directly to see the full error and act on it.", bin),
		}
	}
}

// checkResolution confirms that a way to read the primary screen's
// resolution is present and working, since qubesome sizes a profile from
// it. It deliberately stops at running the binary and does not parse a
// resolution out of the output, since that would be a second
// implementation of the parsing qubesome already does elsewhere, and the
// two could disagree.
func checkResolution(env Env) Check {
	for _, bin := range []string{files.XrandrBinary, files.WlrRandrBinary} {
		if _, err := env.LookPath(bin); err != nil {
			continue
		}

		out, err := env.Output(bin)
		if err != nil {
			return Check{
				Name:   "screen resolution",
				Status: Fail,
				Detail: firstLine(string(out)),
				Fix:    fmt.Sprintf("Run `%s` directly to see the full error and act on it.", bin),
			}
		}

		return Check{
			Name:   "screen resolution",
			Status: OK,
			Detail: fmt.Sprintf("%s reports the screen layout", bin),
		}
	}

	return Check{
		Name:   "screen resolution",
		Status: Fail,
		Detail: "neither xrandr nor wlr-randr was found",
		Fix:    "Install xrandr (or wlr-randr on Wayland) so qubesome can size a profile from the primary screen.",
	}
}

// checkHostDisplay confirms that a profile has a host X server window to
// run inside of.
func checkHostDisplay(env Env) Check {
	display := env.Getenv("DISPLAY")
	if display == "" {
		return Check{
			Name:   "host display",
			Status: Fail,
			Detail: "DISPLAY is not set",
			Fix:    "A profile is a window on the host X server, and there is none to attach to. Run qubesome from within a graphical desktop session.",
		}
	}

	number, ok := displayNumber(display)
	if !ok {
		return Check{
			Name:   "host display",
			Status: Fail,
			Detail: fmt.Sprintf("DISPLAY is malformed: %q", display),
			Fix:    "DISPLAY should look like :0 or :0.0. Check what set it and correct it.",
		}
	}

	socket := "/tmp/.X11-unix/X" + number
	if _, err := env.Stat(socket); err != nil {
		return Check{
			Name:   "host display",
			Status: Fail,
			Detail: fmt.Sprintf("%s does not exist", socket),
			Fix:    "The X server does not appear to be running. Check that your desktop session started one.",
		}
	}

	return Check{
		Name:   "host display",
		Status: OK,
		Detail: fmt.Sprintf("DISPLAY=%s and %s exists", display, socket),
	}
}

// displayNumber extracts the display number from a DISPLAY value such as
// :0 or :0.0, returning ok=false if the value does not start with a colon
// or has no parseable number.
func displayNumber(display string) (string, bool) {
	if !strings.HasPrefix(display, ":") {
		return "", false
	}

	rest := strings.TrimPrefix(display, ":")
	if i := strings.Index(rest, "."); i >= 0 {
		rest = rest[:i]
	}

	if rest == "" {
		return "", false
	}

	if _, err := strconv.Atoi(rest); err != nil {
		return "", false
	}

	return rest, true
}

// checkRenderNode reports whether a GPU render node is available. Its
// absence is a Warn, not a Fail, since qubesome runs without a GPU, just
// with workloads falling back to software rendering.
func checkRenderNode(env Env) Check {
	const (
		driDir   = "/dev/dri"
		renderID = "/dev/dri/renderD128"
	)

	if _, err := env.Stat(driDir); err != nil {
		return Check{
			Name:   "gpu render node",
			Status: Warn,
			Detail: fmt.Sprintf("%s was not found", driDir),
			Fix:    "Workloads will fall back to software rendering, which works but is slower.",
		}
	}

	if _, err := env.Stat(renderID); err != nil {
		return Check{
			Name:   "gpu render node",
			Status: Warn,
			Detail: fmt.Sprintf("%s was not found", renderID),
			Fix:    "Workloads will fall back to software rendering, which works but is slower.",
		}
	}

	return Check{
		Name:   "gpu render node",
		Status: OK,
		Detail: fmt.Sprintf("%s is available", renderID),
	}
}

// dbusLaunchBinary is the last resort the dbus client falls back to when
// no session bus is already running. It is looked up on PATH rather than
// by absolute path, because that is how the client invokes it.
const dbusLaunchBinary = "dbus-launch"

// checkDbus reports whether qubesome can reach a session bus, which is
// where it posts desktop notifications.
//
// qubesome speaks to the bus directly rather than through a helper
// binary, so the question is not whether something is installed but
// whether an address can be found. The order below is the one the client
// itself searches in. Reporting on anything else would say the bus is
// reachable when it is not.
func checkDbus(env Env) Check {
	const name = "desktop notifications"

	// An address of exactly "autolaunch:" names no bus. The client
	// treats it as unset and carries on searching, so this does too.
	if addr := env.Getenv("DBUS_SESSION_BUS_ADDRESS"); addr != "" && addr != "autolaunch:" {
		return Check{
			Name:   name,
			Status: OK,
			Detail: "DBUS_SESSION_BUS_ADDRESS names a session bus",
		}
	}

	// /run/user/<uid>/bus is the socket itself. Its neighbour
	// dbus-session is a file naming one, and older desktops write that
	// instead. Either answers the question.
	runtimeDir := "/run/user/" + env.UID()
	for _, base := range []string{"bus", "dbus-session"} {
		path := filepath.Join(runtimeDir, base)
		if _, err := env.Stat(path); err == nil {
			return Check{
				Name:   name,
				Status: OK,
				Detail: fmt.Sprintf("%s is present", path),
			}
		}
	}

	if _, err := env.LookPath(dbusLaunchBinary); err == nil {
		return Check{
			Name:   name,
			Status: Warn,
			Detail: "no session bus is running, one would be started on demand",
			Fix: "A bus started this way is not the desktop's own, so the notifications sent to it " +
				"may never be shown. Run qubesome from within a desktop session that provides a bus.",
		}
	}

	return Check{
		Name:   name,
		Status: Warn,
		Detail: "no session bus was found and none can be started",
		Fix: "qubesome will log notifications instead of showing them. Run it from within a desktop " +
			"session, or install dbus so a bus can be started on demand.",
	}
}

// checkQubesomeDir reports on the qubesome config directory.
func checkQubesomeDir(env Env) Check {
	dir := files.QubesomeDir()

	fi, err := env.Stat(dir)
	if err != nil {
		return Check{
			Name:   "qubesome directory",
			Status: Warn,
			Detail: fmt.Sprintf("%s does not exist yet", dir),
			Fix:    "It will be created on first use.",
		}
	}

	if !fi.IsDir() {
		return Check{
			Name:   "qubesome directory",
			Status: Fail,
			Detail: fmt.Sprintf("%s exists but is not a directory", dir),
			Fix:    fmt.Sprintf("Remove or rename %s so qubesome can create a directory there.", dir),
		}
	}

	return Check{
		Name:   "qubesome directory",
		Status: OK,
		Detail: fmt.Sprintf("%s is a directory", dir),
	}
}

// firstLine returns the first non-empty line of s, trimmed of surrounding
// whitespace, so that a multi-line command error does not swamp the
// report.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}

	return ""
}
