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
	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/dbus"
	"github.com/qubesome/cli/internal/drive"
	"github.com/qubesome/cli/internal/env"
	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/inception"
	"github.com/qubesome/cli/internal/resolution"
	"github.com/qubesome/cli/internal/socket"
	"github.com/qubesome/cli/internal/types"
	"github.com/qubesome/cli/internal/xauth"
	"golang.org/x/sys/execabs"
)

var (
	ContainerNameFormat = "qubesome-%s"

	profileImage = "ghcr.io/qubesome/xorg:latest"
)

func Run(opts ...command.Option[Options]) error {
	o := &Options{}
	for _, opt := range opts {
		opt(o)
	}

	if o.GitURL != "" {
		return StartFromGit(o.Profile, o.GitURL, o.Path, o.Local)
	}

	if o.Config == nil {
		return fmt.Errorf("cannot start profile: nil config")
	}
	profile, ok := o.Config.Profiles[o.Profile]
	if !ok {
		return fmt.Errorf("cannot start profile: profile %q not found", o.Profile)
	}

	return Start(profile, o.Config)
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
	if err != nil {
		return false
	}

	return true
}

func StartFromGit(name, gitURL, path, local string) error {
	ln := files.ProfileConfig(name)
	if _, err := os.Lstat(ln); err == nil {
		return fmt.Errorf("profile %q is already started", name)
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

	if local != "" && validGitDir(local) {
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

	p, ok := cfg.Profiles[name]
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

	return Start(p, cfg)
}

func Start(profile *types.Profile, cfg *types.Config) (err error) {
	if cfg == nil {
		return fmt.Errorf("cannot start profile: config is nil")
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
				err = dbus.Notify("qubesome start error", fmt.Sprintf("required drive %s is not mounted at %s", split[0], split[1]))
				slog.Debug("failed to notify", "error", err)

				return fmt.Errorf("required drive %q is not mounted at %q", split[0], split[1])
			}

			env.Add(label, split[2])
		}
	}

	wg := &sync.WaitGroup{}
	wg.Add(1)

	go func() {
		err1 := socket.Listen(profile, cfg, inception.HandleConnection)
		if err1 != nil {
			slog.Debug("error listening to socket", "error", err1)
			if err == nil {
				err = err1
			}
		}
		wg.Done()
	}()

	defer func() {
		if fn, err := files.SocketPath(profile.Name); err != nil {
			_ = os.Remove(fn)
		}
	}()

	err = createMagicCookie(profile)
	if err != nil {
		return err
	}

	err = createNewDisplay(profile, strconv.Itoa(int(profile.Display)))
	if err != nil {
		return err
	}

	name := fmt.Sprintf(ContainerNameFormat, profile.Name)
	err = startWindowManager(name, strconv.Itoa(int(profile.Display)), profile.WindowManager)
	if err != nil {
		return err
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

func startWindowManager(name, display, wm string) error {
	args := []string{"exec", name, files.ShBinary, "-c", fmt.Sprintf("DISPLAY=:%s %s", display, wm)}

	slog.Debug(files.DockerBinary+" exec", "container-name", name, "args", args)
	cmd := execabs.Command(files.DockerBinary, args...) //nolint

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", output, err)
	}
	return nil
}

func createNewDisplay(profile *types.Profile, display string) error {
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
	paths = append(paths, "-v=/etc/machine-id:/etc/machine-id:ro")
	paths = append(paths, "-v=/tmp/.X11-unix:/tmp/.X11-unix:rw")
	paths = append(paths, fmt.Sprintf("-v=%s:/tmp/qube.sock:ro", socket))
	paths = append(paths, fmt.Sprintf("-v=%s:/home/xorg-user/.Xserver:ro", server))
	paths = append(paths, fmt.Sprintf("-v=%s:/home/xorg-user/.Xauthority:ro", workload))
	paths = append(paths, fmt.Sprintf("-v=%s:/usr/local/bin/qubesome:ro", binPath))

	for _, p := range profile.Paths {
		paths = append(paths, "-v="+env.Expand(p))
	}

	dockerArgs := []string{
		"run",
		"--rm",
		"-d",
		"-e", "DISPLAY=:0",
		"--network=none",
		"--security-opt=no-new-privileges",
		"--cap-drop=ALL",
	}
	if profile.HostAccess.Gpus != "" {
		dockerArgs = append(dockerArgs, "--gpus", profile.HostAccess.Gpus)
	}

	dockerArgs = append(dockerArgs, paths...)

	dockerArgs = append(dockerArgs, fmt.Sprintf("--name=%s", fmt.Sprintf(ContainerNameFormat, profile.Name)))
	dockerArgs = append(dockerArgs, profileImage)
	dockerArgs = append(dockerArgs, command)
	dockerArgs = append(dockerArgs, cArgs...)

	slog.Debug("exec: docker", "args", dockerArgs)
	cmd := execabs.Command(files.DockerBinary, dockerArgs...) //nolint

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", output, err)
	}
	return nil
}
