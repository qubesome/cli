package profiles

import (
	"fmt"
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
	"github.com/qubesome/cli/internal/drive"
	"github.com/qubesome/cli/internal/env"
	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/images"
	"github.com/qubesome/cli/internal/runners/util/container"
	"github.com/qubesome/cli/internal/types"
	"github.com/qubesome/cli/internal/util/dbus"
	"github.com/qubesome/cli/internal/util/gpu"
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

	if o.Config == nil {
		return fmt.Errorf("cannot start profile: nil config")
	}
	profile, ok := o.Config.Profile(o.Profile)
	if !ok {
		return fmt.Errorf("cannot start profile: profile %q not found", o.Profile)
	}

	return Start(o.Runner, profile, o.Config)
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
		// Wayland is not cleaning up profile state after closure.
		if !strings.EqualFold(os.Getenv("XDG_SESSION_TYPE"), "wayland") {
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
			local = os.ExpandEnv("${HOME}" + local[1:])
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

	if p.Runner != "" {
		runner = p.Runner
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

	binary := files.ContainerRunnerBinary(runner)
	fi, err := os.Lstat(binary)
	if err != nil || !fi.Mode().IsRegular() {
		return fmt.Errorf("could not find container runner %q", binary)
	}

	err = images.PullImageIfNotPresent(binary, profile.Image)
	if err != nil {
		return fmt.Errorf("cannot pull profile image: %w", err)
	}

	go images.PreemptWorkloadImages(binary, cfg)

	if profile.Gpus != "" {
		if !gpu.Supported() {
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

	wg := &sync.WaitGroup{}
	wg.Add(1)

	sockPath, err := files.SocketPath(profile.Name)
	if err != nil {
		return err
	}

	go func() {
		defer wg.Done()

		server := inception.NewServer(profile, cfg)
		err1 := server.Listen(sockPath)
		if err1 != nil {
			slog.Debug("error listening to socket", "error", err1)
			if err == nil {
				err = err1
			}
		}
	}()

	defer func() {
		_ = os.Remove(sockPath)
	}()

	err = createMagicCookie(profile)
	if err != nil {
		return err
	}

	err = createNewDisplay(binary, profile, strconv.Itoa(int(profile.Display)))
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

func createNewDisplay(bin string, profile *types.Profile, display string) error {
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

	//nolint
	var paths []string
	paths = append(paths, "-v=/etc/localtime:/etc/localtime:ro")
	paths = append(paths, "-v=/tmp/.X11-unix:/tmp/.X11-unix:rw")
	paths = append(paths, fmt.Sprintf("-v=%s:/tmp/qube.sock:ro", socket))
	paths = append(paths, fmt.Sprintf("-v=%s:/home/xorg-user/.Xserver", server))
	paths = append(paths, fmt.Sprintf("-v=%s:/home/xorg-user/.Xauthority", workload))
	paths = append(paths, fmt.Sprintf("-v=%s:/usr/local/bin/qubesome:ro", binPath))

	for _, p := range profile.Paths {
		paths = append(paths, "-v="+env.Expand(p))
	}

	dockerArgs := []string{
		"run",
		"--rm",
		"-d",
		// rely on currently set DISPLAY.
		"-e", "DISPLAY",
		"-e", "XDG_SESSION_TYPE=X11",
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
		if strings.HasSuffix(bin, "podman") {
			dockerArgs = append(dockerArgs, "--runtime=nvidia.com/gpu=all")
		}
		dockerArgs = append(dockerArgs, "--gpus", profile.HostAccess.Gpus)
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

	paths = append(paths, fmt.Sprintf("-v=%s:/dev/shm", filepath.Join(userDir, "shm")))
	if profile.Dbus {
		paths = append(paths, "-v=/etc/machine-id:/etc/machine-id:ro")
	} else {
		paths = append(paths, fmt.Sprintf("-v=%s:/run/user/1000", userDir))
		paths = append(paths, fmt.Sprintf("-v=%s:/etc/machine-id:ro", machineIDPath))
	}

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

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", output, err)
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
		return fmt.Errorf("failed to create isolated <qubesome>/user path: %w", err)
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

func writeMachineID(path string) error {
	newUUID := uuid.New()
	uuidString := strings.ReplaceAll(newUUID.String(), "-", "")

	err := os.WriteFile(path, []byte(uuidString), files.FileMode)
	if err != nil {
		return fmt.Errorf("failed to write to %s: %w", path, err)
	}
	return nil
}
