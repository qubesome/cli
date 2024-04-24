package docker

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/types"
	"golang.org/x/sys/execabs"
)

var (
	command = "/usr/bin/docker"

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
	cmd := execabs.Command(command, //nolint:gosec
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

	slog.Debug(command+" exec", "container-id", id, "cmd", ew.Workload.Command, "args", ew.Workload.Args)
	cmd := execabs.Command(command, args...)

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

	ndevs, err := namedDevices(wl.NamedDevices)
	if err != nil {
		return fmt.Errorf("failed to get named devices: %w", err)
	}

	var paths []string
	if wl.HostAccess.LocalTime {
		paths = append(paths, "-v=/etc/localtime:/etc/localtime:ro")
	}

	if wl.HostAccess.MachineID {
		paths = append(paths, "-v=/etc/machine-id:/etc/machine-id:ro")
	}

	args := []string{
		"run",
		"--rm",
		"-d",
		"--security-opt", "seccomp=unconfined",
		"-v=/dev/shm:/dev/shm", // TODO: bind it with ipc?
	}

	args = append(args, paths...)

	// Single instance workloads share the name of the workload, which
	// must be unique. Otherwise, let docker assign a new name.
	if wl.SingleInstance {
		args = append(args, fmt.Sprintf("--name=%s", ew.Name))
	}

	// TODO: Split
	if wl.Microphone || wl.Speakers {
		args = append(args, audioParams()...)
	}
	if wl.Camera {
		args = append(args, cameraParams()...)
	}
	if wl.X11 {
		args = append(args, x11Params()...)
		args = append(args, fmt.Sprintf("-e=DISPLAY=:%d", ew.Profile.Display))

		pp, err := files.ClientCookiePath(ew.Profile.Name)
		if err != nil {
			return err
		}
		args = append(args, fmt.Sprintf("-v=%s:/tmp/.Xauthority", pp))
		args = append(args, "-e=XAUTHORITY=/tmp/.Xauthority")
		args = append(args, fmt.Sprintf("-v=/tmp/.X11-unix/X%[1]d:/tmp/.X11-unix/X%[1]d", ew.Profile.Display))
	}

	//nolint
	if wl.Mime {
		uid := os.Getuid()
		pdir := fmt.Sprintf("/var/run/user/%d/qubesome/%s", uid, ew.Profile.Name)

		homedir, err := getHomeDir(wl.Image)
		if err != nil {
			return err
		}

		srcMimeList := filepath.Join(pdir, "mimeapps.list")
		dstMimeList := filepath.Join(homedir, ".local", "share", "applications", "mimeapps.list")
		err = os.WriteFile(srcMimeList, []byte(mimesList), 0o600)
		if err != nil {
			return fmt.Errorf("failed to write mimeapps.list: %w", err)
		}

		args = append(args, fmt.Sprintf("-v=%s:%s:ro", srcMimeList, dstMimeList))

		srcHandler := filepath.Join(pdir, "mime-handler.desktop")
		dstHandler := filepath.Join(homedir, ".local", "share", "applications", "qubesome-default-handler.desktop")

		err = os.WriteFile(srcHandler, []byte(defaultMimeHandler), 0o600)
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

	// Set hostname to be the same as the container name
	args = append(args, "-h", ew.Name)

	if wl.Bluetooth || wl.VarRunUser {
		args = append(args, "-v=/run/user/1000:/run/user/1000")
	}

	if wl.Bluetooth {
		args = append(args, "-v=/sys/class/bluetooth:/sys/class/bluetooth:ro")
	}

	// TODO: Find a way to not use /dev:/dev
	if wl.Camera || len(wl.NamedDevices) > 0 || wl.Smartcard {
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

		src, err := securejoin.SecureJoin(ew.Profile.Path, filepath.Join("homedir", ps[0]))
		if err != nil {
			slog.Warn("failed to mount path", "path", p, "error", err)
			continue
		}

		dst := ps[1]
		args = append(args, fmt.Sprintf("-v=%s:%s", src, dst))
	}

	for _, p := range wl.HomePaths {
		args = append(args, "-v="+os.ExpandEnv(filepath.Join("${HOME}", p)))
	}

	// TODO: Block by profile
	for _, p := range wl.Volumes {
		ps := strings.SplitN(p, ":", 2)
		if len(ps) != 2 {
			slog.Warn("failed to mount path", "path", p)
			continue
		}

		// TODO: Create volume if not exist
		args = append(args, "--mount", fmt.Sprintf("source=%s,target=%s", ps[0], ps[1]))
	}

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

func getHomeDir(image string) (string, error) {
	args := []string{"run", "--rm", image, "ls", "/home"}

	slog.Debug(command + strings.Join(args, " "))
	cmd := execabs.Command(command, args...)

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get home dir: %w", err)
	}

	return filepath.Join("/home", string(bytes.TrimSpace(out))), nil
}

// Map capability vs Env, device, maps required.
// This should enable easier support for podman, docker and microVM

func x11Params() []string {
	return []string{
		"--device=/dev/dri",

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
