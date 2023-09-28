package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"flag"

	"github.com/qubesome/qubesome-cli/internal/config"
	"github.com/qubesome/qubesome-cli/internal/qubesome"
	"gopkg.in/yaml.v3"
)

const (
	configFile = ".qubesome/qubesome.config"
)

var (
	logLevel string
)

func main() {
	in := qubesome.WorkloadInfo{}

	flag.StringVar(&in.Name, "name", "",
		fmt.Sprintf("The name of the workload to be executed. For new workloads use %s import first.", os.Args[0]))
	flag.StringVar(&in.Profile, "profile", "untrusted", "The profile name which will be used to run the workload.")
	flag.StringVar(&logLevel, "log-level", "ERROR", "The level of log information to be shown to the user. Options are: ERROR, INFO, DEBUG and WARN.")
	flag.Parse()

	h := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slogLevel()})
	slog.SetDefault(slog.New(h))

	args := flag.CommandLine.Args()
	if len(args) == 0 {
		usage()
	}

	q := qubesome.New()
	var err error

	cfg, err := loadConfig()
	checkNil(err)

	q.Config = cfg

	switch args[0] {
	case "run":
		err = q.Run(in)
	case "xdg-open":
		err = q.HandleMime(args[1:])
	}

	checkNil(err)
}

func slogLevel() slog.Level {
	switch logLevel {
	case "ERROR":
		return slog.LevelError
	case "INFO":
		return slog.LevelInfo
	case "DEBUG":
		return slog.LevelDebug
	case "WARN":
		return slog.LevelWarn
	}

	panic(fmt.Sprintf("unsupported log level: %s", logLevel))
}

func checkNil(err error) error {
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
	return nil
}

func usage() {
	fmt.Printf(`usage: %s [flags] <command>

Supported commands:
  run: 	 	  Execute qubesome workloads
  xdg-open:   opens a file or URL in the user's configured workload
`, os.Args[0])
	os.Exit(1)
}

func loadConfig() (*config.Config, error) {
	d, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	path := filepath.Join(d, configFile)
	cfg := &config.Config{}

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
