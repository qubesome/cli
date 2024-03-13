package cmd

import (
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"

	"github.com/qubesome/qubesome-cli/internal/qubesome"
	"github.com/qubesome/qubesome-cli/internal/types"
)

func xdgOpenCmd(args []string, cfg *types.Config) error {
	slog.Debug("check whether running inside container")
	if _, err := os.Stat(socketAddress); err == nil {
		slog.Debug("dialing host qubesome", "socket", socketAddress)
		c, err := net.Dial("unix", socketAddress)
		if err != nil {
			return err
		}

		args = append([]string{"xdg-open"}, args...)
		slog.Debug("writing to socket", "args", args)

		_, err = c.Write([]byte(strings.Join(args, " ")))
		if err != nil {
			return err
		}
		return nil
	}

	f := flag.NewFlagSet("", flag.ExitOnError)
	f.Parse(args)

	slog.Debug("cmd", "args", args)

	if len(f.Args()) != 1 {
		xdgOpenUsage()
	}

	q := qubesome.New()
	q.Config = cfg

	return q.HandleMime(f.Args())
}

func xdgOpenUsage() {
	fmt.Printf(`usage: %s xdg-open https://google.com
`, execName)
	os.Exit(1)
}
