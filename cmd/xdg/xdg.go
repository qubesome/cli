package xdg

import (
	"flag"
	"fmt"

	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/qubesome"
	"github.com/qubesome/cli/internal/types"
)

const usage = `usage:
    %[1]s xdg open https://google.com
    %[1]s xdg --profile personal open https://google.com
`

type handler struct {
}

func New() command.Handler[qubesome.Options] {
	return &handler{}
}

func (c *handler) Handle(in command.App) (command.Action[qubesome.Options], []command.Option[qubesome.Options], error) {
	var profile string

	fs := flag.NewFlagSet("", flag.ContinueOnError)
	fs.StringVar(&profile, "profile", "", "The profile name which will be used to run the workload.")

	err := fs.Parse(in.Args())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse args: %w", err)
	}

	if fs.NArg() < 2 || (fs.NArg() > 0 && fs.Arg(0) != "open") {
		in.Usage(usage)
		return nil, nil, nil
	}

	var opts []command.Option[qubesome.Options]
	opts = append(opts, qubesome.WithProfile(profile))
	opts = append(opts, qubesome.WithExtraArgs(fs.Args()[1:]))

	var cfg *types.Config
	if profile != "" {
		cfg = in.ProfileConfig(profile)
	}

	if cfg == nil {
		cfg = in.UserConfig()
	}

	opts = append(opts, qubesome.WithConfig(cfg))

	return c, opts, nil
}

func (c *handler) Run(opts ...command.Option[qubesome.Options]) error {
	return qubesome.XdgRun(opts...)
}
