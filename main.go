package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"flag"

	"github.com/qubesome/qubesome-cli/internal/config"
	"github.com/qubesome/qubesome-cli/internal/qubesome"
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
	homedir string
)

const (
	configFile  = ".qubesome/qubesome.config"
	logFile     = ".qubesome/qubesome.log"
	logFileMode = 0o600
)

func main() {
	in := qubesome.WorkloadInfo{}

	flag.StringVar(&in.Name, "name", "",
		fmt.Sprintf("The name of the workload to be executed. For new workloads use %s import first.", os.Args[0]))
	flag.StringVar(&in.Profile, "profile", "untrusted", "The profile name which will be used to run the workload.")
	flag.Parse()

	cfg, err := loadConfig()
	checkNil(err)

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

	slog.Debug("qubesome called", "args", os.Args, "config", cfg)

	args := flag.CommandLine.Args()
	if len(args) == 0 {
		usage()
	}

	q := qubesome.New()
	q.Config = cfg

	switch args[0] {
	case "run":
		err = q.Run(in)
	case "xdg-open":
		err = q.HandleMime(args[1:])
	}

	checkNil(err)
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

func usage() {
	fmt.Printf(`usage: %s [flags] <command>

Supported commands:
  run: 	 	  Execute qubesome workloads
  xdg-open:   opens a file or URL in the user's configured workload
`, os.Args[0])
	os.Exit(1)
}

func loadConfig() (*config.Config, error) {
	path := filepath.Join(homedir, configFile)
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
