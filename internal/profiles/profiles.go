package profiles

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/qubesome/cli/internal/profiles/socket"
	"github.com/qubesome/cli/internal/types"
	"golang.org/x/sys/execabs"
)

var (
	ProfileDirFormat     = ".qubesome/profiles/%s"
	ServerCookieFormat   = fmt.Sprintf("%s/.Xserver-cookie", ProfileDirFormat)
	WorkloadCookieFormat = fmt.Sprintf("%s/.Xclient-cookie", ProfileDirFormat)
	ContainerNameFormat  = "qubesome-%s"

	profileImage = "ghcr.io/qubesome/xorg:latest"
)

const (
	cookiesFileMode = 0o600
	dirFileMode     = 0o700
	maxWaitTime     = 30 * time.Second
	sleepTime       = 150 * time.Millisecond
)

func Start(profile types.Profile, cfg *types.Config) (err error) {
	wg := &sync.WaitGroup{}
	wg.Add(1)

	go func() {
		err1 := socket.Listen(profile, cfg)
		if err1 != nil && err == nil {
			err = err1
		}
		wg.Done()
	}()

	err = createMagicCookie(profile)
	if err != nil {
		return err
	}

	// Quick sleep which socket is being created.
	time.Sleep(100 * time.Millisecond)
	err = createNewDisplay(profile.Name, strconv.Itoa(int(profile.Display)))
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

func createMagicCookie(profile types.Profile) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	server := filepath.Join(home, fmt.Sprintf(ServerCookieFormat, profile.Name))
	workload := filepath.Join(home, fmt.Sprintf(WorkloadCookieFormat, profile.Name))

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

func createNewDisplay(profile, display string) error {
	command := "Xephyr"
	cArgs := []string{
		":" + display,
		"-title", fmt.Sprintf("qubesome-%s :%s", profile, display),
		"-auth", "/home/xorg-user/.Xserver",
		"-extension", "MIT-SHM",
		"-extension", "XTEST",
		"-nopn",
		"-nolisten", "tcp",
		"-screen", "3440x1440",
		"-resizeable",
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	server := filepath.Join(home, fmt.Sprintf(ServerCookieFormat, profile))
	workload := filepath.Join(home, fmt.Sprintf(WorkloadCookieFormat, profile))
	binPath, err := os.Executable()
	if err != nil {
		slog.Debug("failed to get exec path", "error", err)
		slog.Debug("profile won't be able to open applications")
	}

	var paths []string
	paths = append(paths, "-v=/etc/localtime:/etc/localtime:ro")
	paths = append(paths, "-v=/tmp/.X11-unix:/tmp/.X11-unix:rw")
	paths = append(paths, fmt.Sprintf("-v=/tmp/qube-%s.sock:/tmp/qube.sock:ro", display))
	paths = append(paths, fmt.Sprintf("-v=%s:/home/xorg-user/.Xserver", server))
	paths = append(paths, fmt.Sprintf("-v=%s:/home/xorg-user/.Xauthority", workload))
	paths = append(paths, fmt.Sprintf("-v=%s:/usr/local/bin/qubesome:ro", binPath))

	paths = append(paths, os.ExpandEnv(fmt.Sprintf("-v=${HOME}/.qubesome/profiles/%s/homedir/.config:/home/xorg-user/.config:ro", profile)))
	paths = append(paths, os.ExpandEnv(fmt.Sprintf("-v=${HOME}/.qubesome/profiles/%s/homedir/.local:/home/xorg-user/.local:ro", profile)))

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

	dockerArgs = append(dockerArgs, fmt.Sprintf("--name=%s", fmt.Sprintf(ContainerNameFormat, profile)))
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
