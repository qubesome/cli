package socket

import (
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/qubesome"
	"github.com/qubesome/cli/internal/types"
)

const (
	bufferSize   = 1024
	sockFileMode = 0o600
	dirFileMode  = 0o700
)

func Listen(p *types.Profile, cfg *types.Config) error {
	fn, err := files.SocketPath(p.Name)
	if err != nil {
		return err
	}

	// Removes previous versions of the socket that were not cleaned up.
	if _, err := os.Stat(fn); err == nil {
		_ = os.Remove(fn)
	}

	err = os.MkdirAll(filepath.Dir(fn), dirFileMode)
	if err != nil {
		return err
	}

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
		os.Exit(1)
	}()

	fmt.Printf("listening at: %s\n", fn)
	for {
		// Accept an incoming connection.
		conn, err := socket.Accept()
		if err != nil {
			slog.Error("cannot accept connection", "error", err)
			continue
		}

		// Handle the connection in a separate goroutine.
		go func(conn net.Conn) {
			defer conn.Close()
			// Create a buffer for incoming data.
			buf := make([]byte, bufferSize)

			// Read data from the connection.
			n, err := conn.Read(buf)
			if err != nil {
				slog.Error("cannot read from socket", "error", err)
				return
			}

			fields := strings.Fields(string(buf[:n]))
			slog.Debug("remote command", "fields", fields)

			if len(fields) < 1 {
				return
			}

			q := qubesome.New()
			q.Config = cfg
			in := qubesome.WorkloadInfo{
				Profile: p.Name,
				Path:    p.Path,
			}

			switch fields[0] {
			case "run":
				// TODO: Refactor to avoid code duplication from root.go
				fs := flag.NewFlagSet("", flag.ExitOnError)
				fs.StringVar(&in.Name, "name", "", "")
				fs.String("profile", "", "")
				err := fs.Parse(fields[1:]) // ignore command
				if err != nil {
					slog.Error("failed to parse", "fields", fields, "error", err)
					return
				}

				if fs.NArg() > 0 {
					in.Args = fields[len(fields)-fs.NArg():]
					slog.Debug("extra args", "args", in.Args)
				}

				err = q.Run(in)
				if err != nil {
					slog.Error("failed to run workload: %v", err)
					return
				}
			case "xdg-open":
				fs := flag.NewFlagSet("", flag.ExitOnError)
				err := fs.Parse(fields[1:]) // ignore command
				if err != nil {
					slog.Error("failed to parse", "fields", fields, "error", err)
					return
				}

				if len(fs.Args()) != 1 {
					slog.Error("xdg-open failed: should have single argument")
					return
				}

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
