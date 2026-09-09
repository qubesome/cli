package cli

import (
	"context"
	"fmt"

	"github.com/qubesome/cli/internal/util/gpu"
	"github.com/urfave/cli/v3"
)

func gpuCommand() *cli.Command {
	var printSpec bool

	cmd := &cli.Command{
		Name:  "gpu",
		Usage: "manages how the GPU is shared with workloads",
		Commands: []*cli.Command{
			{
				Name: "status",
				// The runner flag went with Describe's runner argument.
				// There is one sandbox implementation and it takes no
				// arguments from here.
				Usage: "shows how the GPU is shared with profiles and workloads",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					fmt.Println(gpu.Describe())
					return nil
				},
			},
			{
				Name: "setup",
				Usage: "generates a CDI spec sharing the host Vulkan drivers, " +
					"for workload images that do not carry their own",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:        "print",
						Usage:       "write the spec to stdout instead of " + gpu.SpecPath(),
						Destination: &printSpec,
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if printSpec {
						spec, err := gpu.NewSpec("/")
						if err != nil {
							return err
						}

						data, err := spec.YAML()
						if err != nil {
							return err
						}

						fmt.Print(string(data))
						return nil
					}

					if err := gpu.Setup(); err != nil {
						return err
					}

					fmt.Println("CDI spec written to " + gpu.SpecPath())
					return nil
				},
			},
		},
	}

	return cmd
}
