package main

import (
	"fmt"
	"log/slog"
	"os"

	"flag"

	"github.com/qubesome/qubesome-cli/internal/qubesome"
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

	q := qubesome.New()
	err := q.Run(in)
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
