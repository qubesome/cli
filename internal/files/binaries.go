package files

import (
	"fmt"
	"os/exec"
)

func init() { //nolint
	p, err := exec.LookPath("podman")
	if err != nil {
		p2, err := exec.LookPath("docker")
		if err == nil {
			fmt.Println("falling back to docker")
			ContainerRunnerBinary = p2
		}
	}

	ContainerRunnerBinary = p
}

var (
	ContainerRunnerBinary = "/usr/bin/podman"
)

const (
	ShBinary          = "/bin/sh"
	XclipBinary       = "/usr/bin/xclip"
	FireCrackerBinary = "/usr/bin/firecracker"
	XrandrBinary      = "/usr/bin/xrandr"
	DbusBinary        = "/usr/bin/dbus-send"
)
