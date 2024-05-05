package run

import (
	"flag"
	"fmt"

	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/qubesome"
)

const usage = `usage: %s run -profile untrusted chrome
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

	if fs.NArg() < 1 {
		in.Usage(usage)
		return nil, nil, nil
	}

	var opts []command.Option[qubesome.Options]
	opts = append(opts, qubesome.WithWorkload(fs.Arg(0)))
	opts = append(opts, qubesome.WithProfile(profile))

	cfg := in.UserConfig()
	if cfg == nil {
		cfg = in.ProfileConfig(profile)
	}

	opts = append(opts, qubesome.WithConfig(cfg))

	if fs.NArg() > 1 {
		extra := fs.Args()[1:]
		opts = append(opts, qubesome.WithExtraArgs(extra))
	}

	return c, opts, nil
}

func (c *handler) Run(opts ...command.Option[qubesome.Options]) error {
	return qubesome.Run(opts...)
}
