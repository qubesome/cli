package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"log/slog"

	"github.com/qubesome/qubesome-cli/internal/types"
	"gopkg.in/yaml.v3"
)

func init() {
	d, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}
	homedir = d
}

var (
	execName string
	homedir  string

	commands = map[string]func([]string, *types.Config) error{
		"run":      runCmd,
		"xdg-open": xdgOpenCmd,
	}
)

const (
	configFile  = ".qubesome/qubesome.config"
	logFile     = ".qubesome/qubesome.log"
	logFileMode = 0o600
)

func Exec(args []string) {
	execName = args[0]

	cfg, err := loadConfig()
	checkNil(err)

	configureLogging(cfg)

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

func configureLogging(cfg *types.Config) {
	mw := io.MultiWriter(os.Stdout)
	if cfg.Logging.LogToFile {
		f, err := os.OpenFile(
			filepath.Join(homedir, logFile),
			os.O_RDWR|os.O_CREATE|os.O_APPEND, logFileMode)

		checkNil(err)

		mw = io.MultiWriter(mw, f)
	}

	slog.SetDefault(slog.New(
		slog.NewTextHandler(mw,
			&slog.HandlerOptions{Level: slogLevel(cfg.Logging.Level)}),
	))
}

func slogLevel(logLevel string) slog.Level {
	switch logLevel {
	case "ERROR":
		return slog.LevelError
	case "DEBUG":
		return slog.LevelDebug
	case "INFO":
		fallthrough
	default:
		return slog.LevelInfo
	}
}

func checkNil(err error) error {
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
	return nil
}

func rootUsage() {
	fmt.Printf(`usage: %s <command> [flags]

Supported commands:
  run: 	 	  Execute qubesome workloads
  xdg-open:   opens a file or URL in the user's configured workload
`, execName)
	os.Exit(1)
}

func loadConfig() (*types.Config, error) {
	path := filepath.Join(homedir, configFile)
	cfg := &types.Config{}

	if _, err := os.Stat(path); err != nil && os.IsNotExist(err) {
		slog.Debug("qubesome config not found, falling back to default", "path", path)
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	err = yaml.Unmarshal(data, cfg)
	if err != nil {
		return nil, fmt.Errorf("cannot unmarshal qubesome config %q: %w", path, err)
	}

	return cfg, nil
}
