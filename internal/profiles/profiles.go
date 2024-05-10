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
		return StartFromGit(o.Profile, o.GitURL, o.Path)
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

func StartFromGit(name, gitURL, path string) error {
	ln := files.ProfileConfig(name)
	if _, err := os.Lstat(ln); err == nil {
		return fmt.Errorf("profile %q is already started", name)
	}

	dir, err := files.GitDirPath(gitURL)
	if err != nil {
		return err
	}

	fi, err := os.Stat(dir)
	//nolint
	if err == nil {
		if !fi.IsDir() {
			return fmt.Errorf("found file instead of git dir")
		}

		// Confirm the repository exists and is a valid Git repository.
		_, err := git.PlainOpen(dir)
		if err != nil {
			return err
		}
	} else {
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

	server, err := os.OpenFile(serverPath, os.O_RDWR|os.O_TRUNC, files.FileMode)
	if err != nil {
		return fmt.Errorf("failed to create server cookie %q", serverPath)
	}
	defer server.Close()

	client, err := os.OpenFile(workloadPath, os.O_RDWR|os.O_TRUNC, files.FileMode)
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
	args := []string{"exec", name, files.ShBinary, "-c", fmt.Sprintf("DISPLAY=:%s exec %s", display, wm)}

	slog.Debug(files.DockerBinary+" exec", "container-name", name, "args", args)
	cmd := execabs.Command(files.DockerBinary, args...) //nolint

	return cmd.Run()
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

	server, err := files.ServerCookiePath(profile.Name)
	if err != nil {
		return err
	}
	workload, err := files.ClientCookiePath(profile.Name)
	if err != nil {
		return err
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
		"-e", "DISPLAY",
		"--network=none",
		"--security-opt=no-new-privileges",
		"--cap-drop=ALL",
	}

	dockerArgs = append(dockerArgs, paths...)

	dockerArgs = append(dockerArgs, fmt.Sprintf("--name=%s", fmt.Sprintf(ContainerNameFormat, profile.Name)))
	dockerArgs = append(dockerArgs, profileImage)
	dockerArgs = append(dockerArgs, command)
	dockerArgs = append(dockerArgs, cArgs...)

	slog.Debug("exec: docker", "args", dockerArgs)
	cmd := execabs.Command(files.DockerBinary, dockerArgs...) //nolint

	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout

	return cmd.Run()
}
