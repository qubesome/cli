package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"log/slog"

	"github.com/qubesome/qubesome-cli/internal/log"
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
		"images":   imagesCmd,
		"profiles": profilesCmd,
	}
)

const (
	configFile = ".qubesome/qubesome.config"
)

func Exec(args []string) {
	execName = args[0]

	cfg, _ := loadConfig()

	log.Configure(cfg.Logging.Level,
		cfg.Logging.LogToStdout,
		cfg.Logging.LogToFile,
		cfg.Logging.LogToSyslog)

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
  xdg-open:   Opens a file or URL in the user's configured workload
  images:	  Manage workload images
  profiles:	  Manage profiles
`, execName)
	os.Exit(1)
}

func loadConfig() (*types.Config, error) {
	path := filepath.Join(homedir, configFile)
	cfg := &types.Config{}

	if _, err := os.Stat(path); err != nil && errors.Is(err, fs.ErrNotExist) {
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
