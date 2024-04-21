package socket

import (
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/qubesome/cli/internal/qubesome"
	"github.com/qubesome/cli/internal/types"
)

const (
	sockPathFormat = "/tmp/qube-%d.sock"
	bufferSize     = 1024
	sockFileMode   = 0o600
	dirFileMode    = 0o700
)

func Listen(p types.Profile, cfg *types.Config) error {
	fn := fmt.Sprintf(sockPathFormat, p.Display)
	socket, err := net.Listen("unix", fn)
	if err != nil {
		return fmt.Errorf("failed to listen to socket: %w", err)
	}
	defer func() {
		_ = os.Remove(fn)
	}()

	uid := os.Getuid()

	err = os.Chown(fn, uid, uid)
	if err != nil {
		return err
	}
	err = os.Chmod(fn, sockFileMode)
	if err != nil {
		return err
	}

	pdir := fmt.Sprintf("/var/run/user/%d/qubesome/%s", uid, p.Name)
	err = os.MkdirAll(pdir, dirFileMode)
	if err != nil {
		return fmt.Errorf("failed to create profile dir: %w", err)
	}

	err = os.Chown(pdir, uid, uid)
	if err != nil {
		return err
	}
	err = os.Chmod(pdir, dirFileMode)
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
			buf := make([]byte, bufferSize)

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
				err := fs.Parse(fields[1:]) // ignore command
				if err != nil {
					slog.Error("failed to parse", "fields", fields, "error", err)
				}

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
				err := fs.Parse(fields[1:]) // ignore command
				if err != nil {
					slog.Error("failed to parse", "fields", fields, "error", err)
				}

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
