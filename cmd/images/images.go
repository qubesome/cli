package images

import (
	"flag"
	"fmt"

	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/images"
	"github.com/qubesome/cli/internal/types"
)

const usage = `usage:
    %[1]s images pull
    %[1]s images --profile <NAME> pull
`

type handler struct {
}

func New() command.Handler[images.Options] {
	return &handler{}
}

func (c *handler) Handle(in command.App) (command.Action[images.Options], []command.Option[images.Options], error) {
	var profile string
	fs := flag.NewFlagSet("", flag.ContinueOnError)
	fs.StringVar(&profile, "profile", "", "The profile that the existing workload images will be pulled.")
	err := fs.Parse(in.Args())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse args: %w", err)
	}

	if len(fs.Args()) != 1 || fs.Arg(0) != "pull" {
		in.Usage(usage)
		return nil, nil, nil
	}

	var opts []command.Option[images.Options]
	var cfg *types.Config
	if profile != "" {
		cfg = in.ProfileConfig(profile)
	}

	if cfg == nil {
		cfg = in.UserConfig()
	}
	if cfg == nil {
		return nil, nil, fmt.Errorf("no config found")
	}
	opts = append(opts, images.WithConfig(cfg))

	return c, opts, nil
}

func (c *handler) Run(opts ...command.Option[images.Options]) error {
	return images.Run(opts...)
}
