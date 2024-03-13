package cmd

import (
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/qubesome/qubesome-cli/internal/qubesome"
	"github.com/qubesome/qubesome-cli/internal/types"
	"golang.org/x/sys/execabs"
)

func profilesCmd(args []string, cfg *types.Config) error {
	slog.Debug("cmd", "args", args)
	if len(args) < 1 || args[0] != "run" {
		profilesUsage()
	}

	var name string
	f := flag.NewFlagSet("", flag.ExitOnError)
	f.StringVar(&name, "name", "", "")
	err := f.Parse(args[1:])
	if err != nil {
		return err
	}

	if cfg == nil {
		return fmt.Errorf(`err: could not load config`)
	}

	profile, ok := cfg.Profiles[name]
	if !ok {
		return fmt.Errorf("profile %q not found", name)
	}
	profile.Name = name

	// createMagicCookie
	// createNewDisplay
	// startWindowManager

	// wg := &sync.WaitGroup{}
	// wg.Add(2)
	// profile := f.Arg(1)

	// runProfile(profile, wg)
	// exec(profile, wg)

	// wg.Wait()
	// time.Sleep(5 * time.Minute)

	return listenSocket(profile, cfg)
}

func listenSocket(p types.Profile, cfg *types.Config) error {
	fn := fmt.Sprintf("/tmp/qube-%d.sock", p.Display)
	socket, err := net.Listen("unix", fn)
	if err != nil {
		return fmt.Errorf("failed to listen to socket: %w", err)
	}

	uid := os.Getuid()

	err = os.Chown(fn, uid, uid)
	if err != nil {
		return err
	}
	err = os.Chmod(fn, 0o700)
	if err != nil {
		return err
	}

	pdir := fmt.Sprintf("/var/run/user/%d/qubesome/%s", uid, p.Name)
	err = os.MkdirAll(pdir, 0o700)
	if err != nil {
		return fmt.Errorf("failed to create profile dir: %w", err)
	}

	err = os.Chown(pdir, uid, uid)
	if err != nil {
		return err
	}
	err = os.Chmod(pdir, 0o700)
	if err != nil {
		return err
	}

	// Remove the sock file if the process is terminated.
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		os.Remove(fn)
		os.Exit(1)
	}()

	fmt.Printf("listening at: %s\n", fn)
	for {
		// Accept an incoming connection.
		conn, err := socket.Accept()
		if err != nil {
			log.Fatal(err)
		}

		// Handle the connection in a separate goroutine.
		go func(conn net.Conn) {
			defer conn.Close()
			// Create a buffer for incoming data.
			buf := make([]byte, 1024)

			// Read data from the connection.
			n, err := conn.Read(buf)
			if err != nil {
				log.Fatal(err)
			}

			fields := strings.Fields(string(buf[:n]))
			slog.Debug("remote command", "fields", fields)

			if len(fields) < 1 {
				return
			}

			in := qubesome.WorkloadInfo{}
			switch fields[0] {
			case "run":
				// TODO: Refactor to avoid code duplication from root.go
				fs := flag.NewFlagSet("", flag.ExitOnError)
				fs.StringVar(&in.Name, "name", "", "")
				fs.String("profile", "", "")
				fs.Parse(fields[1:]) // ignore command

				q := qubesome.New()
				q.Config = cfg
				in.Profile = p.Name

				if fs.NArg() > 0 {
					in.Args = fields[len(fields)-fs.NArg():]
					slog.Debug("extra args", "args", in.Args)
				}

				err = q.Run(in)
				if err != nil {
					slog.Error("failed to run workload: %v", err)
				}
			case "xdg-open":
				fs := flag.NewFlagSet("", flag.ExitOnError)
				fs.Parse(fields[1:]) // ignore command

				if len(fs.Args()) != 1 {
					slog.Error("xdg-open failed: should have single argument")
				}

				q := qubesome.New()
				q.Config = cfg

				err = q.HandleMime(fs.Args())
				if err != nil {
					slog.Error("failed to run workload: %v", err)
				}
			default:
				slog.Error("unsupported command: %s", "fields", strings.Join(fields, " "))
			}
		}(conn)
	}
}

func startWindowManager(containerId, display string, wg *sync.WaitGroup) error {
	defer wg.Done()
	args := []string{"exec", containerId, "bash", "-c", fmt.Sprintf("DISPLAY=%s exec awesome", display)}

	slog.Debug("docker exec", "container-id", containerId, "args", args)
	cmd := execabs.Command("docker", args...)

	return cmd.Run()
}

func createNewDisplay(profile, display string, wg *sync.WaitGroup) error {
	defer wg.Done()

	image := "ghcr.io/qubesome/xorg:latest"
	command := "Xephyr"
	cArgs := []string{
		"display",
		"-title", fmt.Sprintf("%s %s", profile, display),
		"-auth", "/home/xorg-user/.Xserver",
		"-extension", "MIT-SHM",
		"-extension", "XTEST",
		"-nopn",
		"-nolisten", "tcp",
		"-screen", "3440x1440",
		"-resizeable",
	}

	var paths []string
	paths = append(paths, "-v=/etc/localtime:/etc/localtime:ro")
	// TODO: limit this
	paths = append(paths, "-v=/tmp/.X11-unix:/tmp/.X11-unix:rw")
	paths = append(paths, "-v=/home/paulo/.qubesome/profiles/personal/.Xserver-cookie:/tmp/.Xauthority")
	paths = append(paths, "-v=/home/paulo/git/pjbgf/dotfiles/homedir/.qubesome/profiles/personal/homedir/.config:/home/xorg-user/.config:ro")

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

	// Set hostname to be the same as the container name
	dockerArgs = append(dockerArgs, "-h", profile)

	dockerArgs = append(dockerArgs, fmt.Sprintf("--name=%s", profile))
	dockerArgs = append(dockerArgs, image)
	dockerArgs = append(dockerArgs, command)
	dockerArgs = append(dockerArgs, cArgs...)

	slog.Debug("exec: docker", "args", dockerArgs)
	cmd := execabs.Command("docker", dockerArgs...)

	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout

	return cmd.Run()
}

func profilesUsage() {
	fmt.Printf(`usage: %s profiles run <NAME>`, execName)
	os.Exit(1)
}
