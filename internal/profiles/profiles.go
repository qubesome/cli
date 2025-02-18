package profiles

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/google/uuid"
	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/images"
	"github.com/qubesome/cli/internal/keyring"
	"github.com/qubesome/cli/internal/keyring/backend"
	"github.com/qubesome/cli/internal/runners/util/container"
	"github.com/qubesome/cli/internal/types"
	"github.com/qubesome/cli/internal/util/dbus"
	"github.com/qubesome/cli/internal/util/drive"
	"github.com/qubesome/cli/internal/util/env"
	"github.com/qubesome/cli/internal/util/gpu"
	"github.com/qubesome/cli/internal/util/mtls"
	"github.com/qubesome/cli/internal/util/resolution"
	"github.com/qubesome/cli/internal/util/xauth"
	"github.com/qubesome/cli/pkg/inception"
	"golang.org/x/sys/execabs"
)

var (
	ContainerNameFormat = "qubesome-%s"
	defaultProfileImage = "ghcr.io/qubesome/xorg:latest"
)

func Run(opts ...command.Option[Options]) error {
	o := &Options{}
	for _, opt := range opts {
		opt(o)
	}

	if o.GitURL != "" {
		return StartFromGit(o.Runner, o.Profile, o.GitURL, o.Path, o.Local)
	}

	if o.Local != "" {
		// Running from local, treat the local path as the GITDIR for envvar
		// expansion purposes.
		err := env.Update("GITDIR", o.Local)
		if err != nil {
			return err
		}
	}

	path := filepath.Join(o.Local, o.Path, "qubesome.config")
	if _, err := os.Stat(path); err != nil {
		return err
	}
	cfg, err := types.LoadConfig(path)
	if err != nil {
		return err
	}
	cfg.RootDir = filepath.Dir(path)

	if cfg == nil {
		return fmt.Errorf("cannot start profile: nil config")
	}
	profile, ok := cfg.Profile(o.Profile)
	if !ok {
		return fmt.Errorf("cannot start profile: profile %q not found", o.Profile)
	}

	return Start(o.Runner, profile, cfg)
}

func validGitDir(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}

	if !fi.IsDir() {
		return false
	}

	// Confirm the repository exists and is a valid Git repository.
	_, err = git.PlainOpen(path)
	return err == nil
}

func StartFromGit(runner, name, gitURL, path, local string) error {
	ln := files.ProfileConfig(name)

	if _, err := os.Lstat(ln); err == nil {
		if container.Running(runner, fmt.Sprintf(ContainerNameFormat, name)) {
			return fmt.Errorf("profile %q is already started", name)
		}

		if err = os.Remove(ln); err != nil {
			return fmt.Errorf("failed to remove leftover profile symlink: %w", err)
		}
	}

	dir, err := files.GitDirPath(gitURL)
	if err != nil {
		return err
	}

	if strings.HasPrefix(local, "~") {
		if len(local) > 1 && local[1] == '/' {
			local = filepath.Join(os.ExpandEnv("${HOME}"), local[1:])
		}
	}

	if local != "" && validGitDir(local) { //nolint
		slog.Debug("start from local", "path", local)
		dir = local
	} else if validGitDir(dir) {
		slog.Debug("start from existing cloned repo", "path", dir)
	} else {
		slog.Debug("cloning repo to start")

		var auth transport.AuthMethod
		if strings.HasPrefix(gitURL, "git@") {
			a, err := ssh.NewSSHAgentAuth("git")
			if err != nil {
				return err
			}
			auth = a
		}

		_, err = git.PlainClone(dir, false, &git.CloneOptions{
			URL:  gitURL,
			Auth: auth,
		})
		if err != nil {
			return err
		}
	}

	// This is a Git setup, and now we know the Git Dir, so enable
	// environment variable GITDIR expansion.
	err = env.Update("GITDIR", dir)
	if err != nil {
		return err
	}

	// Get the qubesome config from the Git repository.
	cfgPath, err := securejoin.SecureJoin(dir, filepath.Join(path, "qubesome.config"))
	if err != nil {
		return err
	}

	cfg, err := types.LoadConfig(cfgPath)
	if err != nil {
		return err
	}

	err = os.MkdirAll(filepath.Dir(ln), files.DirMode)
	if err != nil {
		return err
	}

	err = os.Symlink(cfgPath, ln)
	if err != nil {
		return err
	}
	defer func() {
		_ = os.Remove(ln)
	}()

	p, ok := cfg.Profile(name)
	if !ok {
		return fmt.Errorf("cannot file profile %q in config %q", name, cfgPath)
	}

	// When sourcing from git, ensure profile path is relative to the git repository.
	pp, err := securejoin.SecureJoin(filepath.Dir(cfgPath), p.Path)
	if err != nil {
		return err
	}
	p.Path = pp

	slog.Debug("start from git", "profile", p.Name, "p", path, "path", p.Path, "config", cfgPath)

	return Start(runner, p, cfg)
}

func Start(runner string, profile *types.Profile, cfg *types.Config) (err error) {
	if cfg == nil {
		return fmt.Errorf("cannot start profile: config is nil")
	}

	if profile.Image == "" {
		slog.Debug("no profile image set, using default instead", "default-image", defaultProfileImage)
		profile.Image = defaultProfileImage
	}

	if err := profile.Validate(); err != nil {
		return err
	}

	// If runner is not being overwritten (via -runner), use the runner
	// set at profile level in the config.
	if runner == "" && profile.Runner != "" {
		runner = profile.Runner
	}

	binary := files.ContainerRunnerBinary(runner)
	fi, err := os.Lstat(binary)
	if err != nil || !fi.Mode().IsRegular() {
		return fmt.Errorf("could not find container runner %q", binary)
	}

	imgs, err := images.MissingImages(binary, cfg)
	if err != nil {
		return err
	}

	for _, img := range imgs {
		if img == profile.Image {
			fmt.Println("Pulling profile image:", profile.Image)
			err = images.PullImageIfNotPresent(binary, profile.Image)
			if err != nil {
				return fmt.Errorf("cannot pull profile image: %w", err)
			}
		}
	}

	if os.Stdin != nil && len(imgs) > 1 {
		if proceed("Not all workload images are present. Start loading them on the background?") {
			go images.PreemptWorkloadImages(binary, cfg)
		}
	}

	if profile.Gpus != "" {
		if _, ok := gpu.Supported(runner); !ok {
			profile.Gpus = ""
			dbus.NotifyOrLog("qubesome error", "GPU support was not detected, disabling it for qubesome")
		}
	}

	// Block profile from starting if external drive is not available.
	if len(profile.ExternalDrives) > 0 {
		slog.Debug("profile has required external drives", "drives", profile.ExternalDrives)
		for _, dm := range profile.ExternalDrives {
			split := strings.Split(dm, ":")
			if len(split) != 3 {
				return fmt.Errorf("cannot enforce external drive: invalid format")
			}

			label := split[0]
			ok, err := drive.Mounts(split[1], split[2])
			if err != nil {
				return fmt.Errorf("cannot check drive label mounts: %w", err)
			}

			if !ok {
				dbus.NotifyOrLog("qubesome start error", fmt.Sprintf("required drive %s is not mounted at %s", split[0], split[1]))

				return fmt.Errorf("required drive %q is not mounted at %q", split[0], split[1])
			}

			env.Add(label, split[2])
		}
	}

	err = createMagicCookie(profile)
	if err != nil {
		return err
	}

	wg := &sync.WaitGroup{}
	wg.Add(1)

	sockPath, err := files.SocketPath(profile.Name)
	if err != nil {
		return err
	}

	creds, err := mtls.NewCredentials()
	if err != nil {
		return err
	}
	go func() {
		defer wg.Done()

		server := inception.NewServer(profile, cfg)
		err1 := server.Listen(creds.ServerCert, creds.CA, sockPath)
		if err1 != nil {
			slog.Debug("error listening to socket", "error", err1)
			if err == nil {
				err = err1
			}
		}
	}()

	defer func() {
		// Clean up the profile dir once profile finishes.
		pd := files.ProfileDir(profile.Name)
		err = os.RemoveAll(pd)
		if err != nil {
			slog.Warn("failed to remove profile dir", "path", pd, "error", err)
		}
	}()

	defer func() {
		err := deleteMtlsData(profile.Name)
		if err != nil {
			slog.Warn("failed to delete mTLS data", "error", err)
		}
	}()

	err = createNewDisplay(binary,
		creds.CA, creds.ClientPEM, creds.ClientKeyPEM,
		profile, strconv.Itoa(int(profile.Display)))
	if err != nil {
		return err
	}

	// In Wayland, Xephyr is replaced by xwayland-run, which can
	// run the Window Manager directly, without the need of a exec
	// into the container to trigger it.
	if !strings.EqualFold(os.Getenv("XDG_SESSION_TYPE"), "wayland") {
		name := fmt.Sprintf(ContainerNameFormat, profile.Name)

		// If xhost access control is enabled, it may block qubesome
		// execution. A tail sign is the profile container dying early.
		if !container.Running(binary, name) {
			msg := os.ExpandEnv("run xhost +SI:localhost:${USER} and try again")
			dbus.NotifyOrLog("qubesome start error", msg)
			return fmt.Errorf("failed to start profile: %s", msg)
		}

		err = startWindowManager(binary, name, strconv.Itoa(int(profile.Display)), profile.WindowManager)
		if err != nil {
			return err
		}
	}

	wg.Wait()
	return nil
}

func proceed(prompt string) bool {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("%s (Y/N): ", prompt)
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("Error reading input, please try again.")
			continue
		}

		input = strings.TrimSpace(input)
		if strings.EqualFold(input, "Y") {
			return true
		} else if strings.EqualFold(input, "N") {
			return false
		}
		fmt.Println("Invalid input. Please enter 'Y' or 'N'.")
	}
}

func createMagicCookie(profile *types.Profile) error {
	serverPath, err := files.ServerCookiePath(profile.Name)
	if err != nil {
		return err
	}
	workloadPath, err := files.ClientCookiePath(profile.Name)
	if err != nil {
		return err
	}

	err = os.MkdirAll(filepath.Dir(serverPath), files.DirMode)
	if err != nil {
		return err
	}

	server, err := os.OpenFile(serverPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, files.FileMode)
	if err != nil {
		return fmt.Errorf("failed to create server cookie %q", serverPath)
	}
	defer server.Close()

	client, err := os.OpenFile(workloadPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, files.FileMode)
	if err != nil {
		return fmt.Errorf("failed to create workload cookie %q", workloadPath)
	}
	defer client.Close()

	xauthority := os.Getenv("XAUTHORITY")
	if xauthority == "" {
		return fmt.Errorf("XAUTHORITY not defined")
	}

	slog.Debug("opening parent xauthority", "path", xauthority)
	parent, err := os.Open(xauthority)
	if err != nil {
		return err
	}
	defer parent.Close()

	return xauth.AuthPair(profile.Display, parent, server, client)
}

func startWindowManager(bin, name, display, wm string) error {
	args := []string{"exec", name, files.ShBinary, "-c", fmt.Sprintf("DISPLAY=:%s %s", display, wm)}

	slog.Debug(bin+" exec", "container-name", name, "args", args)
	cmd := execabs.Command(bin, args...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", output, err)
	}
	return nil
}

func createNewDisplay(bin string, ca, cert, key []byte, profile *types.Profile, display string) error {
	command := "Xephyr"
	res, err := resolution.Primary()
	if err != nil {
		return err
	}
	cArgs := []string{
		":" + display,
		"-title", fmt.Sprintf("qubesome-%s :%s", profile.Name, display),
		"-auth", "/home/xorg-user/.Xserver",
		"-extension", "MIT-SHM",
		"-extension", "XTEST",
		"-nopn",
		"-nolisten", "tcp",
		"-screen", res,
		"-resizeable",
	}
	if profile.XephyrArgs != "" {
		cArgs = append(cArgs, strings.Split(profile.XephyrArgs, " ")...)
	}

	if strings.EqualFold(os.Getenv("XDG_SESSION_TYPE"), "wayland") {
		command = "xwayland-run"
		cArgs = []string{
			"-host-grab",
			"-geometry", res,
			"-extension", "MIT-SHM",
			"-extension", "XTEST",
			"-nopn",
			"-tst",
			"-nolisten", "tcp",
			"-auth", "/home/xorg-user/.Xserver",
			"--",
			strings.TrimPrefix(profile.WindowManager, "exec ")}
	}

	server, err := files.ServerCookiePath(profile.Name)
	if err != nil {
		return err
	}
	workload, err := files.ClientCookiePath(profile.Name)
	if err != nil {
		return err
	}

	// If no server cookie is found or it is empty, fail safe.
	if fi, err := os.Stat(server); err != nil || fi.Size() == 0 {
		return fmt.Errorf("server cookie was found")
	}

	binPath, err := os.Executable()
	if err != nil {
		slog.Debug("failed to get exec path", "error", err)
		slog.Debug("profile won't be able to open applications")
	}

	socket, err := files.SocketPath(profile.Name)
	if err != nil {
		return err
	}

	t := time.Now().Add(3 * time.Second)
	for {
		if t.Before(time.Now()) {
			return fmt.Errorf("time out waiting for socket to be created")
		}

		fi, err := os.Stat(socket)
		if err != nil {
			continue
		}
		if fi.IsDir() {
			return fmt.Errorf("socket %q cannot be a dir", socket)
		}
		break
	}

	x11Dir := "/tmp/.X11-unix"
	if os.Getenv("WSL_DISTRO_NAME") != "" {
		fmt.Println("\033[33mWARN: Running qubesome in WSL is experimental. Some features may not work as expected.\033[0m")
		fp, err := filepath.EvalSymlinks(x11Dir)
		if err != nil {
			return fmt.Errorf("failed to eval symlink: %w", err)
		}
		x11Dir = fp
	}

	//nolint
	var paths []string
	paths = append(paths, "-v=/etc/localtime:/etc/localtime:ro")
	paths = append(paths, fmt.Sprintf("-v=%s:/tmp/.X11-unix:rw", x11Dir))
	paths = append(paths, fmt.Sprintf("-v=%s:/tmp/qube.sock:ro", socket))
	paths = append(paths, fmt.Sprintf("-v=%s:/home/xorg-user/.Xserver", server))
	paths = append(paths, fmt.Sprintf("-v=%s:/home/xorg-user/.Xauthority", workload))
	paths = append(paths, fmt.Sprintf("-v=%s:/usr/local/bin/qubesome:ro", binPath))

	for _, p := range profile.Paths {
		p = env.Expand(p)

		src := strings.Split(p, ":")
		if _, err := os.Stat(src[0]); err != nil {
			fmt.Printf("\033[33mWARN: missing mapped dir: %s.\033[0m\n", src[0])
		}
		paths = append(paths, "-v="+p)
	}

	dockerArgs := []string{
		"run",
		"--rm",
		"-d",
		// rely on currently set DISPLAY.
		"-e", "DISPLAY",
		"-e", "XDG_SESSION_TYPE=X11",
		"-e", "Q_MTLS_CA",
		"-e", "Q_MTLS_CERT",
		"-e", "Q_MTLS_KEY",
		"--device", "/dev/dri",
		"--security-opt=no-new-privileges:true",
		"--cap-drop=ALL",
	}

	if strings.HasSuffix(bin, "podman") {
		dockerArgs = append(dockerArgs, "--userns=keep-id")
	}
	if strings.EqualFold(os.Getenv("XDG_SESSION_TYPE"), "wayland") {
		xdgRuntimeDir := os.Getenv("XDG_RUNTIME_DIR")
		if xdgRuntimeDir == "" {
			uid := os.Getuid()
			if uid < 1000 {
				return fmt.Errorf("qubesome does not support running under privileged users")
			}
			xdgRuntimeDir = "/run/user/" + strconv.Itoa(uid)
		}

		// TODO: Investigate ways to avoid sharing /run/user/1000 on Wayland.
		dockerArgs = append(dockerArgs, "-e XDG_RUNTIME_DIR")
		dockerArgs = append(dockerArgs, "-v="+xdgRuntimeDir+":/run/user/1000")
	}
	if profile.HostAccess.Gpus != "" {
		if gpus, ok := gpu.Supported(profile.Runner); ok {
			dockerArgs = append(dockerArgs, gpus)
		}
	}

	if profile.DNS != "" {
		dockerArgs = append(dockerArgs, "--dns", profile.DNS)
	}

	if profile.Network == "" {
		// Generally, xorg does not require network access so by
		// default sets network to none.
		dockerArgs = append(dockerArgs, "--network=none")
	} else {
		dockerArgs = append(dockerArgs, "--network="+profile.Network)
	}

	// Write the machine-id file regardless of the profile using host dbus or not,
	// as this will enable workloads to use either approach.
	machineIDPath := filepath.Join(files.ProfileDir(profile.Name), "machine-id")
	err = writeMachineID(machineIDPath)
	if err != nil {
		return fmt.Errorf("failed to write machine-id: %w", err)
	}

	userDir, err := files.IsolatedRunUserPath(profile.Name)
	if err != nil {
		return fmt.Errorf("failed to get isolated <qubesome>/user path: %w", err)
	}
	err = setupRunUserDir(userDir)
	if err != nil {
		return err
	}

	err = setupAppsDir(profile)
	if err != nil {
		return err
	}

	paths = append(paths, fmt.Sprintf("-v=%s:/dev/shm", filepath.Join(userDir, "shm")))
	if profile.Dbus {
		paths = append(paths, "-v=/etc/machine-id:/etc/machine-id:ro")
	} else {
		paths = append(paths, fmt.Sprintf("-v=%s:/run/user/1000", userDir))
		paths = append(paths, fmt.Sprintf("-v=%s:/etc/machine-id:ro", machineIDPath))
	}

	paths = append(paths, fmt.Sprintf("-v=%s:/home/xorg-user/.local/share/applications:ro",
		filepath.Join(files.ProfileDir(profile.Name), "applications")))
	paths = append(paths, fmt.Sprintf("-v=%s:/home/xorg-user/.local/share/icons:ro",
		filepath.Join(files.ProfileDir(profile.Name), "icons")))

	dockerArgs = append(dockerArgs, paths...)

	// Share IPC from the profile container to its workloads.
	// dockerArgs = append(dockerArgs, "--ipc=shareable")
	dockerArgs = append(dockerArgs, "--shm-size=128m")

	dockerArgs = append(dockerArgs, fmt.Sprintf("--name=%s", fmt.Sprintf(ContainerNameFormat, profile.Name)))
	dockerArgs = append(dockerArgs, profile.Image)
	dockerArgs = append(dockerArgs, command)
	dockerArgs = append(dockerArgs, cArgs...)

	fmt.Println(
		"INFO: For best experience use input grabber shortcuts:",
		grabberShortcut())

	slog.Debug("exec: "+bin, "args", dockerArgs)
	cmd := execabs.Command(bin, dockerArgs...)
	cmd.Env = append(os.Environ(), "Q_MTLS_CA="+string(ca))
	cmd.Env = append(cmd.Env, "Q_MTLS_CERT="+string(cert))
	cmd.Env = append(cmd.Env, "Q_MTLS_KEY="+string(key))

	err = storeMtlsData(profile.Name, string(ca), string(cert), string(key))
	if err != nil {
		return err
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", output, err)
	}
	return nil
}

func storeMtlsData(profile, ca, cert, key string) error {
	ks := keyring.New(profile, backend.New())
	if err := ks.Set(keyring.MtlsCA, ca); err != nil {
		return err
	}

	if err := ks.Set(keyring.MtlsClientCert, cert); err != nil {
		return err
	}

	if err := ks.Set(keyring.MtlsClientKey, key); err != nil {
		return err
	}
	return nil
}

func deleteMtlsData(profile string) error {
	ks := keyring.New(profile, backend.New())
	if err := ks.Delete(keyring.MtlsCA); err != nil {
		return err
	}
	if err := ks.Delete(keyring.MtlsClientCert); err != nil {
		return err
	}
	if err := ks.Delete(keyring.MtlsClientKey); err != nil {
		return err
	}
	return nil
}

func grabberShortcut() string {
	if strings.EqualFold(os.Getenv("XDG_SESSION_TYPE"), "wayland") {
		return "<Super> + <Esc>"
	}

	return "<Ctrl> + <Shift>"
}

func setupRunUserDir(dir string) error {
	err := os.MkdirAll(dir, files.DirMode)
	if err != nil {
		return fmt.Errorf("failed to create isolated <profile>/user dir: %w", err)
	}

	shm := filepath.Join(dir, "shm")
	err = os.MkdirAll(shm, 0o777)
	if err != nil {
		return fmt.Errorf("failed to create profile shm dir: %w", err)
	}

	err = os.Chmod(shm, 0o1777)
	if err != nil {
		return fmt.Errorf("failed to chmod profile shm dir: %w", err)
	}

	return nil
}

func setupAppsDir(profile *types.Profile) error {
	dir := files.ProfileDir(profile.Name)
	err := os.MkdirAll(filepath.Join(dir, "applications"), files.DirMode)
	if err != nil {
		return fmt.Errorf("failed to create <profile>/applications dir: %w", err)
	}
	err = os.MkdirAll(filepath.Join(dir, "icons"), files.DirMode)
	if err != nil {
		return fmt.Errorf("failed to create <profile>/icons dir: %w", err)
	}

	for _, name := range profile.Flatpaks {
		src := filepath.Join(files.FlatpakApps(), name+".desktop")
		target := filepath.Join(dir, "applications", name+".desktop")

		err = processFlatPakFile(name, src, target)
		if err != nil {
			slog.Error("failed processing flatpak desktop file", "name", name, "error", err)
			continue
		}

		src = filepath.Join(files.FlatpakIcons(), name+".svg")
		target = filepath.Join(dir, "icons", name+".svg")
		err = copyFlatPakIcon(src, target)
		if err != nil {
			slog.Error("failed copying flatpak icon", "name", name, "error", err)
		}

		slog.Debug("added flatpak workload", "name", name)
	}
	return nil
}

func copyFlatPakIcon(sourcePath, destPath string) error {
	srcFile, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer srcFile.Close()

	destFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, srcFile)
	if err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}

	err = destFile.Sync()
	if err != nil {
		return fmt.Errorf("failed to sync file: %w", err)
	}

	return nil
}

func processFlatPakFile(workload, src, target string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer srcFile.Close()

	destFile, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("failed to create new file: %w", err)
	}
	defer destFile.Close()

	scanner := bufio.NewScanner(srcFile)
	writer := bufio.NewWriter(destFile)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Exec=") {
			line = fmt.Sprintf("Exec=/bin/sh -c '/usr/local/bin/qubesome flatpak run \"%s\"'", workload)
		} else if strings.HasPrefix(line, "Icon=") {
			line = fmt.Sprintf("Icon=~/.local/share/icons/%s.svg", workload)
		}

		_, err := writer.WriteString(line + "\n")
		if err != nil {
			return fmt.Errorf("failed to write to destination file: %w", err)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading source file: %w", err)
	}

	err = writer.Flush()
	if err != nil {
		return fmt.Errorf("failed to flush writer: %w", err)
	}

	return nil
}

func writeMachineID(path string) error {
	newUUID := uuid.New()
	uuidString := strings.ReplaceAll(newUUID.String(), "-", "")

	err := os.WriteFile(path, []byte(uuidString), files.FileMode)
	if err != nil {
		return fmt.Errorf("failed to write to %s: %w", path, err)
	}
	return nil
}
