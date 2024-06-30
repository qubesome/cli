package docker

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/qubesome/cli/internal/dbus"
	"github.com/qubesome/cli/internal/env"
	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/gpu"
	"github.com/qubesome/cli/internal/types"
	"golang.org/x/sys/execabs"
)

var (
	defaultMimeHandler = `[Desktop Entry]
Version=1.0
Type=Application
Name=qubesome Mime Handler
Exec=/usr/local/bin/qubesome xdg-open %u
StartupNotify=false
`
	mimesList = `[Default Applications]
x-scheme-handler/slack=qubesome-default-handler.desktop;

application/x-yaml=qubesome-default-handler.desktop;
text/english=qubesome-default-handler.desktop;
text/html=qubesome-default-handler.desktop;
text/plain=qubesome-default-handler.desktop;
text/x-c=qubesome-default-handler.desktop;
text/x-c++=qubesome-default-handler.desktop;
text/x-makefile=qubesome-default-handler.desktop;
text/xml=qubesome-default-handler.desktop;
x-www-browser=qubesome-default-handler.desktop;

x-scheme-handler/http=qubesome-default-handler.desktop;
x-scheme-handler/https=qubesome-default-handler.desktop;
x-scheme-handler/about=qubesome-default-handler.desktop;
x-scheme-handler/unknown=qubesome-default-handler.desktop;

[Removed Associations]
x-scheme-handler/slack=slack.desktop;
x-scheme-handler/http=firefox.desktop;
x-scheme-handler/https=firefox.desktop;
x-scheme-handler/snap=snap-handle-link.desktop;
`
)

func ContainerID(name string) (string, bool) {
	args := fmt.Sprintf("ps -a -q -f name=%s", name)
	cmd := execabs.Command(files.DockerBinary, //nolint:gosec
		strings.Split(args, " ")...)

	out, err := cmd.Output()
	id := string(bytes.TrimSuffix(out, []byte("\n")))

	if err != nil || id == "" {
		return "", false
	}

	return id, true
}

func exec(id string, ew types.EffectiveWorkload) error {
	args := []string{"exec", "--detach", id, ew.Workload.Command}
	args = append(args, ew.Workload.Args...)

	slog.Debug(files.DockerBinary+" exec", "container-id", id, "cmd", ew.Workload.Command, "args", ew.Workload.Args)
	cmd := execabs.Command(files.DockerBinary, args...) //nolint

	return cmd.Run()
}

func Run(ew types.EffectiveWorkload) error {
	if err := ew.Validate(); err != nil {
		return err
	}

	wl := ew.Workload
	if wl.SingleInstance {
		if id, ok := ContainerID(ew.Name); ok {
			return exec(id, ew)
		}
	}

	ndevs, err := namedDevices(wl.USBDevices)
	if err != nil {
		return fmt.Errorf("failed to get named devices: %w", err)
	}

	if wl.Gpus != "" {
		if !gpu.Supported() {
			wl.Gpus = ""
			dbus.NotifyOrLog("qubesome error", "GPU support was not detected, disabling it for qubesome")
		}
	}

	var paths []string
	if wl.HostAccess.LocalTime {
		// Mount localtime into container. This file may be a symlink, if so,
		// mount the underlying file as well.
		file := "/etc/localtime"
		if _, err := os.Stat(file); err == nil {
			paths = append(paths, fmt.Sprintf("-v=%[1]s:%[1]s:ro", file))

			if target, err := os.Readlink(file); err == nil {
				paths = append(paths, fmt.Sprintf("-v=%[1]s:%[1]s:ro", target))
			}
		}
	}

	args := []string{
		"run",
		"--rm",
		"-d",
		"--security-opt", "seccomp=unconfined",
		"-v=/dev/shm:/dev/shm", // TODO: bind it with ipc?
	}

	if ew.Workload.User != nil {
		args = append(args, fmt.Sprintf("--user=%d", *ew.Workload.User))
	}

	args = append(args, paths...)

	// Single instance workloads share the name of the workload, which
	// must be unique. Otherwise, let docker assign a new name.
	if wl.SingleInstance {
		args = append(args, fmt.Sprintf("--name=%s", ew.Name))
	}

	if wl.HostAccess.Gpus != "" {
		args = append(args, "--gpus", wl.HostAccess.Gpus)
	}

	for _, cap := range wl.CapsAdd {
		args = append(args, "--cap-add="+cap)
	}
	for _, dev := range wl.Devices {
		args = append(args, "--device="+dev)
	}

	// TODO: Split
	if wl.Microphone || wl.Speakers {
		args = append(args, audioParams()...)
	}
	if wl.Camera {
		args = append(args, cameraParams()...)
	}
	if wl.Dbus {
		args = append(args, hostDbusParams()...)
	} else {
		userDir, err := files.IsolatedRunUserPath(ew.Profile.Name)
		if err != nil {
			return fmt.Errorf("failed to get isolated <qubesome>/user path: %w", err)
		}

		paths = append(paths, fmt.Sprintf("-v=%s:/run/user/1000", userDir))

		machineIDPath := filepath.Join(files.ProfileDir(ew.Profile.Name), "machine-id")
		paths = append(paths, fmt.Sprintf("-v=%s:/etc/machine-id:ro", machineIDPath))
	}

	args = append(args, "--device=/dev/dri")

	// Display is used for all qubesome applications.
	args = append(args, fmt.Sprintf("-e=DISPLAY=:%d", ew.Profile.Display))
	pp, err := files.ClientCookiePath(ew.Profile.Name)
	if err != nil {
		return err
	}
	args = append(args, fmt.Sprintf("-v=%s:/tmp/.Xauthority:ro", pp))
	args = append(args, "-e=XAUTHORITY=/tmp/.Xauthority")
	args = append(args, fmt.Sprintf("-v=/tmp/.X11-unix/X%[1]d:/tmp/.X11-unix/X%[1]d", ew.Profile.Display))

	//nolint
	if wl.Mime {
		pdir := files.ProfileDir(ew.Profile.Name)
		homedir, err := getHomeDir(wl.Image)
		if err != nil {
			return err
		}

		srcMimeList := filepath.Join(pdir, "mimeapps.list")
		dstMimeList := filepath.Join(homedir, ".local", "share", "applications", "mimeapps.list")
		err = os.WriteFile(srcMimeList, []byte(mimesList), files.FileMode)
		if err != nil {
			return fmt.Errorf("failed to write mimeapps.list: %w", err)
		}

		args = append(args, fmt.Sprintf("-v=%s:%s:ro", srcMimeList, dstMimeList))
		srcHandler := filepath.Join(pdir, "mime-handler.desktop")
		dstHandler := filepath.Join(homedir, ".local", "share", "applications", "qubesome-default-handler.desktop")

		err = os.WriteFile(srcHandler, []byte(defaultMimeHandler), files.FileMode)
		if err != nil {
			return fmt.Errorf("failed to write mime-handler.desktop: %w", err)
		}
		args = append(args, fmt.Sprintf("-v=%s:%s:ro", srcHandler, dstHandler))

		qubesomeBin, err := os.Executable()
		if err != nil {
			return err
		}

		// Mount access to the qubesome binary.
		args = append(args, fmt.Sprintf("-v=%s:%s:ro", qubesomeBin, "/usr/local/bin/qubesome"))

		socket, err := files.SocketPath(ew.Profile.Name)
		if err != nil {
			return err
		}

		// Mount qube socket so that it can send commands from container to host.
		args = append(args, fmt.Sprintf("-v=%s:/tmp/qube.sock:ro", socket))
	}

	if ew.Profile.DNS != "" {
		args = append(args, "--dns", ew.Profile.DNS)
	}

	// Set hostname to be the same as the container name
	args = append(args, "-h", ew.Name)

	// TODO: what exactly needs to be shared here?
	if wl.Bluetooth || wl.VarRunUser {
		args = append(args, "-v=/run/user/1000:/run/user/1000")
	}

	if wl.Bluetooth {
		args = append(args, "-v=/sys/class/bluetooth:/sys/class/bluetooth:ro")
	}

	// TODO: Find a way to not use /dev:/dev
	if wl.Camera || len(wl.USBDevices) > 0 || wl.Smartcard {
		args = append(args, "-v=/dev:/dev")
	}

	if wl.Network != "" {
		args = append(args, fmt.Sprintf("--network=%s", wl.Network))
	}

	// TODO: Block by profile
	if wl.Privileged {
		args = append(args, "--privileged")
	}

	for _, ndev := range ndevs {
		args = append(args, fmt.Sprintf("--device=%s", ndev))
	}
	if len(ndevs) > 0 {
		args = append(args, "-v=/sys/class/usbmisc:/sys/class/usbmisc")
	}

	for _, p := range wl.Paths {
		ps := strings.SplitN(p, ":", 2)
		if len(ps) != 2 {
			slog.Warn("failed to mount path", "path", p)
			continue
		}

		src := env.Expand(ps[0])
		if _, err := os.Stat(src); err != nil {
			slog.Warn("failed to mount path", "path", src, "error", err)
			continue
		}

		dst := ps[1]
		args = append(args, fmt.Sprintf("-v=%s:%s", src, dst))
	}

	args = append(args, wl.Image)
	args = append(args, wl.Command)
	args = append(args, wl.Args...)

	slog.Debug(fmt.Sprintf("exec: %s", files.DockerBinary), "args", args)
	cmd := execabs.Command(files.DockerBinary, args...) //nolint

	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout

	return cmd.Run()
}

func getHomeDir(image string) (string, error) {
	args := []string{"run", "--rm", image, "ls", "/home"}

	slog.Debug(files.DockerBinary + " " + strings.Join(args, " "))
	cmd := execabs.Command(files.DockerBinary, args...) //nolint

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get home dir: %w", err)
	}

	return filepath.Join("/home", string(bytes.TrimSpace(out))), nil
}

func hostDbusParams() []string {
	return []string{
		"-v=/run/dbus/system_bus_socket:/run/dbus/system_bus_socket",
		"-v=/run/user/1000/bus:/run/user/1000/bus",
		"-v=/var/lib/dbus:/var/lib/dbus",
		"-v=/usr/share/dbus-1:/usr/share/dbus-1",
		"-v=/run/user/1000/dbus-1:/run/user/1000/dbus-1",
		"-e=DBUS_SESSION_BUS_ADDRESS",
		"-e=XDG_RUNTIME_DIR",
		"-e=XDG_SESSION_ID",
	}
}

func cameraParams() []string {
	params := []string{
		"--group-add=video",
	}

	vds, _ := filepath.Glob("/dev/video*")
	for _, dev := range vds {
		params = append(params, fmt.Sprintf("--device=%s", dev))
	}

	return params
}

func audioParams() []string {
	return []string{
		// TODO: For Bluetooth (Apple AirPods) you may require /run/user/1000 shared via VarRunUser
		"-v=/run/user/1000/pipewire-0:/run/user/1000/pipewire-0",
		"--device=/dev/snd",
		"--group-add=audio",
	}
}
