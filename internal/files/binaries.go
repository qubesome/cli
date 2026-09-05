package files

import (
	"log/slog"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const (
	ShBinary          = "/bin/sh"
	XclipBinary       = "/usr/bin/xclip"
	FireCrackerBinary = "/usr/bin/firecracker"
	XrandrBinary      = "/usr/bin/xrandr"
	WlrRandrBinary    = "/usr/bin/wlr-randr"
	DbusBinary        = "/usr/bin/dbus-send"
	PodmanBinary      = "/usr/bin/podman"
	DockerBinary      = "/usr/bin/docker"
)

// runnerDirs holds the directories searched for a container runner, in
// order of preference.
//
// The container runner is the binary that enforces every isolation setting
// qubesome asks for, so it is not resolved through PATH: a directory
// prepended to PATH would decide what runs the workloads.
var runnerDirs = []string{
	"/usr/bin",
	"/usr/local/bin",
	"/bin",
}

// runners holds the supported container runners, in the order they are
// auto-detected.
var runners = []string{"podman", "docker"}

func ContainerRunnerBinary(runner string) string {
	switch runner {
	case "podman", "docker":
		if p, ok := lookRunner(runner); ok {
			return p
		}

		slog.Debug("could not find runner", "runner", runner, "dirs", runnerDirs)
		if runner == "docker" {
			return DockerBinary
		}
		return PodmanBinary
	}

	slog.Debug("auto-detecting runner")
	for _, r := range runners {
		if p, ok := lookRunner(r); ok {
			slog.Debug("found runner", "runner", r, "path", p)
			return p
		}
	}

	slog.Debug("fallback to static path", "path", PodmanBinary)
	return PodmanBinary
}

// lookRunner returns the path of a regular file named name in one of
// runnerDirs that the current user can execute.
//
// Symlinks are resolved, so what is returned is the file that will
// actually run. The directories searched are root owned, so a link placed
// in one of them is the administrator's intent, but callers elsewhere
// check the runner with Lstat and reject anything that is not a regular
// file, and they should not disagree with this one about what was found.
func lookRunner(name string) (string, bool) {
	for _, dir := range runnerDirs {
		p, err := filepath.EvalSymlinks(filepath.Join(dir, name))
		if err != nil {
			continue
		}

		fi, err := os.Lstat(p)
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}

		// The permission bits alone do not say whether this user can
		// execute the file, and a runner that cannot be executed is no
		// better than one that is not there.
		if err := unix.Access(p, unix.X_OK); err != nil {
			slog.Debug("runner is not executable", "path", p, "error", err)
			continue
		}

		return p, true
	}

	return "", false
}
