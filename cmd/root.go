package cmd

import (
	"fmt"
	"os"

	"log/slog"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/log"
	"github.com/qubesome/cli/internal/types"
)

var (
	execName string
	homedir  string

	commands = map[string]func([]string, *types.Config) error{
		"run":       runCmd,
		"xdg-open":  xdgOpenCmd,
		"images":    imagesCmd,
		"start":     startCmd,
		"clipboard": clipboardCmd,
		"deps":      depsCmd,
	}
)

func Exec(args []string) {
	execName = args[0]

	cfg, err := types.LoadConfig(files.QubesomeConfig())
	checkNil(err)

	err = log.Configure(cfg.Logging.Level,
		cfg.Logging.LogToStdout,
		cfg.Logging.LogToFile,
		cfg.Logging.LogToSyslog)
	checkNil(err)

	slog.Debug("qubesome called", "args", args, "config", cfg)
	if len(args) < 2 {
		rootUsage()
	}

	cmd, ok := commands[args[1]]
	if !ok {
		rootUsage()
		os.Exit(1)
	}

	slog.Debug("exec subcommand", args[1], args[2:])
	checkNil(cmd(args[2:], cfg))
}

func checkNil(err error) {
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}

func rootUsage() {
	fmt.Printf(`usage: %s <command> [flags]

Supported commands:
  run: 	 	  Execute qubesome workloads
  xdg-open:   Opens a file or URL in the user's configured workload
  images:	  Manage workload images
  start:	  Start qubesome profiles.
  clipboard:  Enable copying of clipboard from host and between profiles
  deps: 	  Shows status of all dependencies
`, execName)
	os.Exit(1)
}
