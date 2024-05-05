package inception

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/types"
)

var (
	commands = map[string]func(*types.Config, *types.Profile, []string) error{}
)

func Add(cmd string, f func(*types.Config, *types.Profile, []string) error) {
	commands[cmd] = f
}

const (
	bufferSize = 1024
)

func Inside() bool {
	path := files.InProfileSocketPath()
	_, err := os.Stat(path)
	return (err == nil)
}

func RunOnHost(cmd string, args []string) error {
	slog.Debug("check whether running inside container")
	if !Inside() {
		return fmt.Errorf("cannot run against host: socket not found")
	}

	path := files.InProfileSocketPath()
	fmt.Println("dialing host qubesome", "socket", path)
	c, err := net.Dial("unix", path)
	if err != nil {
		return err
	}

	a := append([]string{cmd}, args...)
	command := strings.Join(a, " ")

	fmt.Println("host qubesome run", "command", command)
	_, err = c.Write([]byte(command))
	if err != nil {
		return err
	}
	err = c.Close()
	if err != nil {
		return fmt.Errorf("failed to close socket: %w", err)
	}

	return nil
}

func HandleConnection(cfg *types.Config, p *types.Profile, conn net.Conn) {
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

	cmd, ok := commands[fields[0]]
	if !ok {
		slog.Debug("command not supported", "fields", fields)
		return
	}

	var args []string
	if len(fields) > 1 {
		args = fields[1:]
	}

	err = cmd(cfg, p, args)
	if err != nil {
		slog.Debug("inception error: failed to run command", "fields", fields, "error", err)
	}
}
