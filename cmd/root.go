package cmd

import (
	"fmt"
	"os"

	"log/slog"

	"github.com/qubesome/cli/cmd/clipboard"
	"github.com/qubesome/cli/cmd/deps"
	"github.com/qubesome/cli/cmd/images"
	"github.com/qubesome/cli/cmd/run"
	"github.com/qubesome/cli/cmd/start"
	"github.com/qubesome/cli/cmd/xdg"
	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/log"
	"github.com/qubesome/cli/internal/types"
)

func newConsole() *Console {
	return &Console{
		commands: map[string]func([]string, *types.Config) error{
			"run":       run.Command,
			"xdg-open":  xdg.Command,
			"images":    images.Command,
			"start":     start.Command,
			"clipboard": clipboard.Command,
			"deps":      deps.Command,
		},
	}
}

type Console struct {
	commands map[string]func([]string, *types.Config) error
}

func (*Console) Exit(code int) {
	os.Exit(code)
}

func (*Console) Printf(format string, a ...any) (n int, err error) {
	return fmt.Printf(format, a...)
}

func (c *Console) Command(name string) (func([]string, *types.Config) error, bool) {
	f, ok := c.commands[name]
	return f, ok
}

type App interface {
	Exit(code int)
	Printf(format string, a ...interface{}) (n int, err error)
	Command(name string) (func([]string, *types.Config) error, bool)
}

var (
	// DefaultLogLevel defines the initial log level, which is overriden
	// by any LogLevel defined at the user-level configuration file.
	DefaultLogLevel     = "DEBUG"
	ConsoleApp      App = newConsole()

	homedir string
)

func Exec(args []string) {
	if len(args) == 0 {
		return
	}

	var cfg *types.Config
	var err error

	path := files.QubesomeConfig()
	if _, err = os.Stat(path); err == nil {
		cfg, err = types.LoadConfig(path)
		checkNil(err)

		slog.Debug("global config loaded", "path", path)

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

	cmd, ok := ConsoleApp.Command(args[1])
	if !ok {
		rootUsage(args[0])
		ConsoleApp.Exit(1)
		return
	}

	slog.Debug("exec subcommand", args[1], args[2:])
	checkNil(cmd(args[2:], cfg))
}

func checkNil(err error) {
	if err != nil {
		slog.Error(err.Error())
		ConsoleApp.Exit(1)
	}
}

var usage = `usage: %s <command> [flags]

Supported commands:
  run: 	 	  Execute qubesome workloads
  xdg-open:   Opens a file or URL in the user's configured workload
  images:	  Manage workload images
  start:	  Start qubesome profiles.
  clipboard:  Enable copying of clipboard from host and between profiles
  deps: 	  Shows status of all dependencies
`

func rootUsage(name string) {
	ConsoleApp.Printf(usage, name)
	ConsoleApp.Exit(1)
}
