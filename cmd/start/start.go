package start

import (
	"flag"

	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/profiles"
)

const usage = `usage:
    %[1]s start <profile>
    %[1]s start -git=https://github.com/qubesome/dotfiles-example -path / <profile>
`

type handler struct {
}

func New() command.Handler[profiles.Options] {
	return &handler{}
}

func (c *handler) Handle(in command.App) (command.Action[profiles.Options], []command.Option[profiles.Options], error) {
	var gitURL, path string

	f := flag.NewFlagSet("", flag.ContinueOnError)
	f.StringVar(&gitURL, "git", "", "Defines a git repository source")
	f.StringVar(&path, "path", "", "Dir path that contains qubesome.config")

	err := f.Parse(in.Args())
	if err != nil {
		return nil, nil, err
	}

	var opts []command.Option[profiles.Options]

	if f.NArg() != 1 {
		in.Usage(usage)
		return nil, nil, nil
	}

	name := f.Arg(0)
	if gitURL != "" {
		opts = append(opts, profiles.WithGitURL(gitURL))
	}

	cfg := in.UserConfig()
	if cfg != nil {
		opts = append(opts, profiles.WithConfig(cfg))
	}

	opts = append(opts, profiles.WithProfile(name))
	opts = append(opts, profiles.WithPath(path))

	return c, opts, nil
}

func (c *handler) Run(opts ...command.Option[profiles.Options]) error {
	return profiles.Run(opts...)
}
