package docker

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/qubesome/qubesome-cli/internal/types"
	"golang.org/x/sys/execabs"
)

var (
	command = "docker"
)

func containerId(name string) (string, bool) {
	args := fmt.Sprintf("ps -a -q -f name=%s", name)
	cmd := execabs.Command(command,
		strings.Split(args, " ")...)

	out, err := cmd.Output()
	id := string(bytes.TrimSuffix(out, []byte("\n")))

	if err != nil || id == "" {
		return "", false
	}

	return id, true
}

func exec(id string, ew types.EffectiveWorkload) error {
	args := []string{"exec", id, ew.Workload.Command}
	args = append(args, ew.Workload.Args...)

	slog.Debug("docker exec", "container-id", id, "cmd", ew.Workload.Command, "args", ew.Workload.Args)
	cmd := execabs.Command(command, args...)

	return cmd.Run()
}

func Run(ew types.EffectiveWorkload) error {
	if err := ew.Validate(); err != nil {
		return err
	}

	wl := ew.Workload
	if wl.SingleInstance {
		if id, ok := containerId(ew.Name); ok {
			return exec(id, ew)
		}
	}

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
		"-v=/dev/shm:/dev/shm", // TODO: bind it with ipc?
	}

	args = append(args, paths...)

	// TODO: Split
	if wl.Microphone || wl.Speakers {
		args = append(args, audioParams()...)
	}
	if wl.Camera {
		args = append(args, cameraParams()...)
	}
	if wl.X11 {
		args = append(args, x11Params()...)
	}

	// Set hostname to be the same as the container name
	args = append(args, "-h", ew.Name)

	if wl.VarRunUser {
		args = append(args, "-v=/run/user/1000:/run/user/1000")
	}

	// TODO: Find a way to not use /dev:/dev
	if wl.Camera || len(wl.NamedDevices) > 0 || wl.Smartcard {
		args = append(args, "-v=/dev:/dev")
	}

	if wl.Network != "" {
		args = append(args, fmt.Sprintf("--network=%s", wl.Network))
	}

	for _, ndev := range ndevs {
		args = append(args, fmt.Sprintf("--device=%s", ndev))
	}

	for _, p := range wl.Paths {
		// TODO: Path traversal
		args = append(args, fmt.Sprintf("-v=%s", filepath.Join(ew.Profile.Path, "homedir", p)))
	}

	args = append(args, fmt.Sprintf("--name=%s", ew.Name))
	args = append(args, wl.Image)
	args = append(args, wl.Command)
	args = append(args, wl.Args...)

	slog.Debug(fmt.Sprintf("exec: %s", command), "args", args)
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
		// TODO: For Bluetooth (Apple AirPods) you may require /run/user/1000 shared via VarRunUser
		"-v=/run/user/1000/pipewire-0:/run/user/1000/pipewire-0",
		"--device=/dev/snd",
		"--group-add=audio",
	}
}
