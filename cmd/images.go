package cmd

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/qubesome/qubesome-cli/internal/types"
	"github.com/qubesome/qubesome-cli/internal/util"
	"golang.org/x/sys/execabs"
	"gopkg.in/yaml.v3"
)

func imagesCmd(args []string, cfg *types.Config) error {
	f := flag.NewFlagSet("", flag.ExitOnError)
	f.Parse(args)

	slog.Error("cmd", "args", args)

	if len(f.Args()) != 1 || f.Arg(0) != "pull" {
		imagesUsage()
	}

	if cfg == nil {
		return fmt.Errorf(`err: could not load config`)
	}

	workloadDir, err := util.Path(util.WorkloadsDir)
	if err != nil {
		return fmt.Errorf("cannot get qubesome path: %w", err)
	}

	de, err := os.ReadDir(workloadDir)
	if err != nil {
		return fmt.Errorf("cannot read workloads dir: %w", err)
	}

	seen := map[string]struct{}{}
	for _, w := range de {
		if !w.Type().IsRegular() {
			continue
		}

		fn, err := securejoin.SecureJoin(workloadDir, w.Name())
		if err != nil {
			return fmt.Errorf("cannot join %q and %q: %w", workloadDir, fn, err)
		}

		data, err := os.ReadFile(fn)
		if err != nil {
			return fmt.Errorf("cannot read file %q: %w", fn, err)
		}

		w := types.Workload{}
		err = yaml.Unmarshal(data, &w)
		if err != nil {
			return fmt.Errorf("cannot unmarshal workload file %q: %w", fn, err)
		}

		if _, ok := seen[w.Image]; !ok {
			seen[w.Image] = struct{}{}

			err = pull(w.Image)
			if err != nil {
				slog.Error("cannot pull image %q: %w", w.Image, err)
			}
		}
	}

	return nil
}

func imagesUsage() {
	fmt.Printf(`usage: %s images pull`, execName)
	os.Exit(1)
}

func pull(image string) error {
	slog.Debug("pulling workload image", "image", image)
	cmd := execabs.Command("docker", "pull", image)

	return cmd.Run()
}
