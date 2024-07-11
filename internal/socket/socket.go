package socket

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/types"
)

type ConnectionHandler func(cfg *types.Config, p *types.Profile, conn net.Conn)

func Listen(p *types.Profile, cfg *types.Config, handler ConnectionHandler) error {
	fn, err := files.SocketPath(p.Name)
	if err != nil {
		return err
	}

	// Removes previous versions of the socket that were not cleaned up.
	if _, err := os.Stat(fn); err == nil {
		_ = os.Remove(fn)
	}

	err = os.MkdirAll(filepath.Dir(fn), files.DirMode)
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

	err = os.Chmod(fn, files.FileMode)
	if err != nil {
		return err
	}

	pdir := fmt.Sprintf("/run/user/%d/qubesome/%s", uid, p.Name)
	err = os.MkdirAll(pdir, files.DirMode)
	if err != nil {
		return fmt.Errorf("failed to create profile dir: %w", err)
	}

	err = os.Chmod(pdir, files.DirMode)
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

	slog.Debug("listening profile socket", "path", fn)
	for {
		// Accept an incoming connection.
		conn, err := socket.Accept()
		if err != nil {
			slog.Error("cannot accept connection", "error", err)
			continue
		}

		// Handle the connection in a separate goroutine.
		go handler(cfg, p, conn)
	}
}
