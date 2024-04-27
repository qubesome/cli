package profiles

import (
	"errors"
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
	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/profiles/socket"
	"github.com/qubesome/cli/internal/types"
	"golang.org/x/sys/execabs"
)

var (
	ContainerNameFormat = "qubesome-%s"

	profileImage = "ghcr.io/qubesome/xorg:latest"
)

const (
	cookiesFileMode = 0o600
	dirFileMode     = 0o700
	maxWaitTime     = 30 * time.Second
	sleepTime       = 150 * time.Millisecond
)

func StartFromGit(name, gitURL, path string) error {
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

		r, err := git.PlainOpen(dir)
		if err != nil {
			return err
		}

		wt, err := r.Worktree()
		if err != nil {
			return err
		}

		err = wt.Pull(&git.PullOptions{
			Force: true,
		})
		if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
			return fmt.Errorf("failed to pull latest: %w", err)
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

	// Get the qubesome config from the Git repository.
	cfgPath, err := securejoin.SecureJoin(dir, filepath.Join(path, "qubesome.config"))
	if err != nil {
		return err
	}

	cfg, err := types.LoadConfig(cfgPath)
	if err != nil {
		return err
	}

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
	wg := &sync.WaitGroup{}
	wg.Add(1)

	go func() {
		err1 := socket.Listen(profile, cfg)
		if err != nil && err == nil {
			err = err1
		}
		wg.Done()
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
	err = startWindowManager(name, strconv.Itoa(int(profile.Display)))
	if err != nil {
		return err
	}

	wg.Wait()
	return nil
}

func createMagicCookie(profile *types.Profile) error {
	server, err := files.ServerCookiePath(profile.Name)
	if err != nil {
		return err
	}
	workload, err := files.ClientCookiePath(profile.Name)
	if err != nil {
		return err
	}

	err = os.MkdirAll(filepath.Dir(server), dirFileMode)
	if err != nil {
		return err
	}

	// If previous cookies exist, remove them.
	err = os.WriteFile(server, []byte{}, cookiesFileMode)
	if err != nil {
		return fmt.Errorf("failed to ensure clean server cookie %q", server)
	}

	err = os.WriteFile(workload, []byte{}, cookiesFileMode)
	if err != nil {
		return fmt.Errorf("failed to ensure clean workload cookie %q", workload)
	}

	cmd := execabs.Command("/usr/bin/mcookie")
	cookie, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("cannot create auth cookie for profile %q: %w", profile.Name, err)
	}

	slog.Debug("/usr/bin/xauth", "args",
		[]string{"-f", server, "add", ":" + strconv.Itoa(int(profile.Display)), ".", string(cookie)})
	cmd = execabs.Command(
		"/usr/bin/xauth", "-f", server, "add", ":"+strconv.Itoa(int(profile.Display)), ".", string(cookie))
	cmd.Env = append(cmd.Env, fmt.Sprintf("XAUTHORITY=%q", server))

	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("cannot authorise cookie for profile %q: %w", profile.Name, err)
	}

	args := []string{
		"-c",
		fmt.Sprintf("XAUTHORITY=%q", server) + " " +
			"/usr/bin/xauth nlist :" + strconv.Itoa(int(profile.Display)) +
			" | /usr/bin/sed -e 's/^..../ffff/' " +
			" | " + fmt.Sprintf("XAUTHORITY=%q", server) + " " +
			"/usr/bin/xauth -f " + workload + " nmerge -",
	}
	slog.Debug("/usr/bin/sh", "args", args)
	cmd = execabs.Command("/usr/bin/sh", args...)

	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("cannot merge cookie auth for profile %q: %w", profile.Name, err)
	}

	return nil
}

func startWindowManager(name, display string) error {
	args := []string{"exec", name, "bash", "-c", fmt.Sprintf("DISPLAY=:%s exec awesome", display)}

	slog.Debug("docker exec", "container-name", name, "args", args)
	cmd := execabs.Command("/usr/bin/docker", args...)

	return cmd.Run()
}

func createNewDisplay(profile *types.Profile, display string) error {
	command := "Xephyr"
	cArgs := []string{
		":" + display,
		"-title", fmt.Sprintf("qubesome-%s :%s", profile.Name, display),
		"-auth", "/home/xorg-user/.Xserver",
		"-extension", "MIT-SHM",
		"-extension", "XTEST",
		"-nopn",
		"-nolisten", "tcp",
		"-screen", "3440x1440",
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
		paths = append(paths, "-v="+filepath.Join(profile.Path, p))
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
	cmd := execabs.Command("/usr/bin/docker", dockerArgs...)

	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout

	return cmd.Run()
}