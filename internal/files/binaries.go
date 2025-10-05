package files

import (
	"log/slog"
	"os/exec"
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

func ContainerRunnerBinary(runner string) string {
	switch runner {
	case "podman":
		p, err := exec.LookPath("podman")
		if err == nil {
			return p
		}

		slog.Debug("could not find podman on PATH", "binary", PodmanBinary)
		return PodmanBinary
	case "docker":
		p, err := exec.LookPath("docker")
		if err == nil {
			return p
		}

		slog.Debug("could not find docker on PATH", "binary", DockerBinary)
		return DockerBinary
	}

	slog.Debug("auto-detecting runner")
	p, err := exec.LookPath("podman")
	if err == nil {
		slog.Debug("found podman", "path", p)
		return p
	}

	p, err = exec.LookPath("docker")
	if err == nil {
		slog.Debug("found docker", "path", p)
		return p
	}

	slog.Debug("fallback to static path", "path", PodmanBinary)
	return PodmanBinary
}
