package images

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/qubesome/cli/internal/types"
)

func Command(args []string, cfg *types.Config) error {
	f := flag.NewFlagSet("", flag.ExitOnError)
	err := f.Parse(args)
	if err != nil {
		return fmt.Errorf("failed to parse args: %w", err)
	}

	slog.Debug("cmd", "args", args)

	if len(f.Args()) != 1 || f.Arg(0) != "pull" {
		imagesUsage(os.Args[0])
	}

	if cfg == nil {
		return fmt.Errorf("err: could not load config")
	}

	return types.PullAll()
}

func imagesUsage(name string) {
	fmt.Printf("usage: %s images pull", name)
	os.Exit(1)
}