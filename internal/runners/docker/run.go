package docker

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/keyring"
	"github.com/qubesome/cli/internal/keyring/backend"
	"github.com/qubesome/cli/internal/runners/util/container"
	"github.com/qubesome/cli/internal/runners/util/mime"
	"github.com/qubesome/cli/internal/runners/util/usb"
	"github.com/qubesome/cli/internal/types"
	"github.com/qubesome/cli/internal/util/dbus"
	"github.com/qubesome/cli/internal/util/env"
	"github.com/qubesome/cli/internal/util/gpu"
	"golang.org/x/sys/execabs"
)

var runnerBinary = files.ContainerRunnerBinary("docker")

func Run(ew types.EffectiveWorkload) error {
	if err := ew.Validate(); err != nil {
		return err
	}

	wl := ew.Workload
	if wl.SingleInstance {
		if id, ok := container.ID(runnerBinary, ew.Name); ok {
			return container.Exec(runnerBinary, id, ew)
		}
	}

	ndevs, err := usb.NamedDevices(wl.HostAccess.USBDevices)
	if err != nil {
		return fmt.Errorf("failed to get named devices: %w", err)
	}

	var paths []string
	// Mount localtime into container. This file may be a symlink, if so,
	// mount the underlying file as well.
	file := "/etc/localtime"
	if _, err := os.Stat(file); err == nil {
		paths = append(paths, fmt.Sprintf("-v=%[1]s:%[1]s:ro", file))

		if target, err := os.Readlink(file); err == nil {
			paths = append(paths, fmt.Sprintf("-v=%[1]s:%[1]s:ro", target))
		}
	}

	args := []string{
		"run",
		"--rm",
		"-d",
		"--security-opt=seccomp=unconfined",
		"--security-opt=label=disable",
		"--security-opt=no-new-privileges=true",
	}

	if ew.Workload.User != nil {
		args = append(args, fmt.Sprintf("--user=%d", *ew.Workload.User))
	}

	// Single instance workloads share the name of the workload, which
	// must be unique. Otherwise, let docker assign a new name.
	if wl.SingleInstance {
		args = append(args, fmt.Sprintf("--name=%s", ew.Name))
	}

	if wl.HostAccess.Gpus != "" {
		gpu, ok := gpu.Supported("podman")
		if !ok {
			wl.HostAccess.Gpus = ""
			dbus.NotifyOrLog("qubesome error", "GPU support was not detected, disabling it for qubesome")
		} else {
			args = append(args, gpu)
		}
	}

	for _, cap := range wl.HostAccess.CapsAdd {
		args = append(args, "--cap-add="+cap)
	}
	for _, dev := range wl.HostAccess.Devices {
		args = append(args, "--device="+dev)
	}

	// TODO: Split
	if wl.HostAccess.Microphone || wl.HostAccess.Speakers {
		args = append(args, audioParams()...)
	}
	if wl.HostAccess.Camera {
		args = append(args, cameraParams()...)
	}

	display := ew.Profile.Display
	if strings.EqualFold(os.Getenv("XDG_SESSION_TYPE"), "wayland") { //nolint
		display = 0

		xdgRuntimeDir := os.Getenv("XDG_RUNTIME_DIR")
		if xdgRuntimeDir == "" {
			uid := os.Getuid()
			if uid < 1000 {
				return fmt.Errorf("qubesome does not support running under privileged users")
			}
			xdgRuntimeDir = "/run/user/" + strconv.Itoa(uid)
		}

		// TODO: Investigate ways to avoid sharing /run/user/1000 on Wayland.
		args = append(args, "-e", "XDG_RUNTIME_DIR")
		args = append(args, "-e", "XDG_BACKEND")
		args = append(args, "-e", "XDG_SEAT")
		args = append(args, "-e", "XDG_SESSION_TYPE")
		args = append(args, "-e", "XDG_SESSION_ID")
		args = append(args, "-e", "XDG_SESSION_CLASS")
		args = append(args, "-e", "XDG_SESSION_DESKTOP")
		args = append(args, "-e", "WAYLAND_DISPLAY")
		args = append(args, "-e", "HYPRLAND_INSTANCE_SIGNATURE")
		args = append(args, "-e", "DBUS_SESSION_BUS_ADDRESS")

		args = append(args, "-v="+xdgRuntimeDir+":/run/user/1000")
	} else {
		if wl.HostAccess.Dbus || wl.HostAccess.Bluetooth || wl.HostAccess.VarRunUser {
			args = append(args, "-v=/run/user/1000:/run/user/1000")
		}

		userDir, err := files.IsolatedRunUserPath(ew.Profile.Name)
		if err != nil {
			return fmt.Errorf("failed to get isolated <qubesome>/user path: %w", err)
		}
		paths = append(paths, fmt.Sprintf("-v=%s:/dev/shm", filepath.Join(userDir, "shm")))
		if wl.HostAccess.Dbus || wl.HostAccess.Bluetooth || wl.HostAccess.VarRunUser {
			args = append(args, hostDbusParams()...)
		} else {
			paths = append(paths, fmt.Sprintf("-v=%s:/run/user/1000", userDir))

			machineIDPath := filepath.Join(files.ProfileDir(ew.Profile.Name), "machine-id")
			paths = append(paths, fmt.Sprintf("-v=%s:/etc/machine-id:ro", machineIDPath))
		}
	}

	args = append(args, paths...)
	args = append(args, "--device=/dev/dri")

	// Display is used for all qubesome applications.
	args = append(args, fmt.Sprintf("-e=DISPLAY=:%d", display))
	pp, err := files.ClientCookiePath(ew.Profile.Name)
	if err != nil {
		return err
	}
	args = append(args, fmt.Sprintf("-v=%s:/tmp/.Xauthority:ro", pp))
	args = append(args, "-e=XAUTHORITY=/tmp/.Xauthority")
	args = append(args, fmt.Sprintf("-v=/tmp/.X11-unix/X%[1]d:/tmp/.X11-unix/X%[1]d", display))
	args = append(args, fmt.Sprintf("-e=QUBESOME_PROFILE=%s", ew.Profile.Name))

	if ew.Profile.Timezone != "" {
		args = append(args, "-e=TZ="+ew.Profile.Timezone)
	}

	args = append(args, "--init")
	// Link to the profiles IPC.
	// args = append(args, fmt.Sprintf("--ipc=container:qubesome-%s", ew.Profile.Name))

	//nolint
	if wl.HostAccess.Mime {
		pdir := files.ProfileDir(ew.Profile.Name)
		homedir, err := getHomeDir(wl.Image)
		if err != nil {
			return err
		}

		err = os.MkdirAll(pdir, files.DirMode)
		if err != nil {
			return fmt.Errorf("failed to ensure profile dir: %w", err)
		}

		srcMimeList := filepath.Join(pdir, "mimeapps.list")
		dstMimeList := filepath.Join(homedir, ".local", "share", "applications", "mimeapps.list")
		err = os.WriteFile(srcMimeList, []byte(mime.MimesList), files.FileMode)
		if err != nil {
			return fmt.Errorf("failed to write mimeapps.list: %w", err)
		}

		args = append(args, fmt.Sprintf("-v=%s:%s:ro", srcMimeList, dstMimeList))
		srcHandler := filepath.Join(pdir, "mime-handler.desktop")
		dstHandler := filepath.Join(homedir, ".local", "share", "applications", "qubesome-default-handler.desktop")

		err = os.WriteFile(srcHandler, []byte(mime.DefaultMimeHandler), files.FileMode)
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
		args = append(args, "-e=Q_MTLS_CA")
		args = append(args, "-e=Q_MTLS_CERT")
		args = append(args, "-e=Q_MTLS_KEY")
	}

	if ew.Profile.DNS != "" {
		args = append(args, "--dns", ew.Profile.DNS)
	}

	// Set hostname to be the same as the container name
	args = append(args, "-h", ew.Name)

	if wl.HostAccess.Network != "" {
		args = append(args, fmt.Sprintf("--network=%s", wl.HostAccess.Network))
	}

	if wl.HostAccess.Privileged {
		args = append(args, "--privileged")
	}

	if len(ndevs) > 0 {
		// Some USB devices, such as YubiKeys, requires --device pointing to both
		// the hidraw device as well as the respective /dev/usb. The latter by
		// itself would enable things such as  "ykinfo -a". However, use of SK keys
		// fails with operation not permitted unless /dev:/dev is also mapped.
		args = append(args, "-v=/dev/:/dev/")

		for _, ndev := range ndevs {
			args = append(args, fmt.Sprintf("--device=%s", ndev))
		}
	}

	for _, p := range wl.HostAccess.Paths {
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

	slog.Debug(fmt.Sprintf("exec: %s", runnerBinary), "args", args)
	cmd := execabs.Command(runnerBinary, args...)

	if ew.Workload.HostAccess.Mime {
		// Since the implementation of mTLS, workloads granted mime handling
		// need the mTLS creds so that they can communicate with the inception
		// server.

		if ca, cert, key, ok := mtlsData(ew.Profile.Name); ok {
			slog.Debug("mime access: enabled")

			cmd.Env = append(os.Environ(), "Q_MTLS_CA="+ca)
			cmd.Env = append(cmd.Env, "Q_MTLS_CERT="+cert)
			cmd.Env = append(cmd.Env, "Q_MTLS_KEY="+key)
		} else {
			slog.Debug("mime access: skipped")
		}
	}

	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout

	return cmd.Run()
}

func mtlsData(name string) (string, string, string, bool) {
	ks := keyring.New(name, backend.New())
	ca, err := ks.Get(keyring.MtlsCA)
	if err != nil {
		slog.Error("failed to fetch mtls-ca", "error", err)
		return "", "", "", false
	}

	cert, err := ks.Get(keyring.MtlsClientCert)
	if err != nil {
		slog.Error("failed to fetch mtls-client-cert", "error", err)
		return "", "", "", false
	}

	key, err := ks.Get(keyring.MtlsClientKey)
	if err != nil {
		slog.Error("failed to fetch mtls-client-key", "error", err)
		return "", "", "", false
	}

	return ca, cert, key, true
}

func getHomeDir(image string) (string, error) {
	args := []string{"run", "--rm", image, "ls", "/home"}

	slog.Debug(runnerBinary + " " + strings.Join(args, " "))
	cmd := execabs.Command(runnerBinary, args...)

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get home dir: %w", err)
	}

	return filepath.Join("/home", string(bytes.TrimSpace(out))), nil
}

func hostDbusParams() []string {
	return []string{
		"-v=/run/dbus/system_bus_socket:/run/dbus/system_bus_socket",
		"-v=/var/lib/dbus:/var/lib/dbus",
		"-v=/usr/share/dbus-1:/usr/share/dbus-1",
		// At the moment we are mapping /run/user/1000 when
		// the host Dbus is being used. Therefore, there is no
		// point in mounting descending dirs.
		// "-v=/run/user/1000/bus:/run/user/1000/bus",
		// "-v=/run/user/1000/dbus-1:/run/user/1000/dbus-1",
		"-v=/etc/machine-id:/etc/machine-id:ro",
		"-e=DBUS_SESSION_BUS_ADDRESS",
		"-e=XDG_RUNTIME_DIR",
		"-e=XDG_SESSION_ID",
	}
}

func cameraParams() []string {
	//nolint:prealloc
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
