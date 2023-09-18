package docker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/qubesome/qubesome-cli/internal/workload"
	"golang.org/x/sys/execabs"
)

var (
	command  = "docker"
	basePath = "/home/paulo/.qubesome/profiles"
)

func Run(wl workload.Effective) error {
	ndevs, err := namedDevices(wl.NamedDevices)
	if err != nil {
		return fmt.Errorf("failed to get named devices: %w", err)
	}

	//TODO: auto map profiles dir
	paths := []string{
		"-v=/etc/localtime:/etc/localtime:ro",
		"-v=/etc/machine-id:/etc/machine-id:ro",
	}

	args := []string{
		"run",
		"--rm",
		"-d",
		"--security-opt", "seccomp=unconfined",
		"-v=/dev:/dev",         // required for camera, or named devices
		"-v=/dev/shm:/dev/shm", // TODO: bind it with ipc?
	}

	args = append(args, paths...)

	if wl.Audio {
		args = append(args, audioParams()...)
	}
	if wl.Camera {
		args = append(args, cameraParams()...)
	}
	if wl.X11 {
		args = append(args, x11Params()...)
	}

	for _, ndev := range ndevs {
		args = append(args, fmt.Sprintf("--device=%s", ndev))
	}

	for _, p := range wl.Path {
		// TODO: Path traversal
		args = append(args, fmt.Sprintf("-v=%s", filepath.Join(basePath, wl.Profile, "homedir", p)))
	}

	args = append(args, fmt.Sprintf("--name=%s-%s", wl.Name, wl.Profile))
	args = append(args, wl.Image)
	args = append(args, wl.Command)
	args = append(args, wl.Args...)

	fmt.Println(command, strings.Join(args, " "))

	cmd := execabs.Command(command, args...)

	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout

	return cmd.Run()
}

// Map capability vs Env, device, maps required.
// This should enable easier support for podman, docker and microVM

func x11Params() []string {
	return []string{
		"--device=/dev/dri",

		"-v=/tmp/.X11-unix:/tmp/.X11-unix",
		"-v=/run/dbus/system_bus_socket:/run/dbus/system_bus_socket",
		"-v=/run/user/1000/bus:/run/user/1000/bus",
		"-v=/var/lib/dbus:/var/lib/dbus",
		"-v=/run/user/1000/dbus-1:/run/user/1000/dbus-1",

		"-e=DISPLAY",
		"-e=DBUS_SESSION_BUS_ADDRESS",
		"-e=XDG_RUNTIME_DIR",
		"-e=XDG_SESSION_ID",
	}
}

func cameraParams() []string {
	return []string{
		"--device=/dev/video0",
		"--group-add=video",
	}
}

func audioParams() []string {
	return []string{
		"-v=/run/user/1000/pipewire-0:/run/user/1000/pipewire-0",
		"--device=/dev/snd",
		"--group-add=audio",
	}
}
