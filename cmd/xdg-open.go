package cmd

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/qubesome/qubesome-cli/internal/qubesome"
	"github.com/qubesome/qubesome-cli/internal/types"
)

func xdgOpenCmd(args []string, cfg *types.Config) error {
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
