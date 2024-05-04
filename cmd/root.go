package cmd

import (
	"log/slog"

	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/log"
)

var (
	// DefaultLogLevel defines the initial log level, which is overriden
	// by any LogLevel defined at the user-level configuration file.
	DefaultLogLevel             = "DEBUG"
	ConsoleApp      command.App = newConsole()

	homedir string
)

func Exec(args []string) {
	if len(args) == 0 {
		return
	}

	var err error
	cfg := ConsoleApp.UserConfig()

	if cfg != nil {
		err = log.Configure(cfg.Logging.Level,
			cfg.Logging.LogToStdout,
			cfg.Logging.LogToFile,
			cfg.Logging.LogToSyslog)
		checkNil(err)
	} else {
		// If no config found is found, enable stdout log for
		// improved troubleshooting.
		err = log.Configure(DefaultLogLevel, true, false, false)
	}

	slog.Debug("qubesome called", "args", args, "config", cfg)
	if len(args) < 2 {
		rootUsage(args[0])
		return
	}

	ok := ConsoleApp.Command(args[1])
	if !ok {
		rootUsage(args[0])
		ConsoleApp.Exit(1)
		return
	}

	err = ConsoleApp.RunSubCommand()
	checkNil(err)
}

func checkNil(err error) {
	if err != nil {
		slog.Error(err.Error())
		ConsoleApp.Exit(1)
	}
}

const usage = `usage: %s <command> [flags]

Supported commands:
  run: 	 	  Execute qubesome workloads
  xdg:   Opens a file or URL in the user's configured workload
  images:	  Manage workload images
  start:	  Start qubesome profiles.
  clipboard:  Enable copying of clipboard from host and between profiles
  deps: 	  Shows status of all dependencies
`

func rootUsage(name string) {
	ConsoleApp.Printf(usage, name)
	ConsoleApp.Exit(1)
}
