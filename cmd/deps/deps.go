package deps

import (
	"flag"

	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/deps"
)

const usage = `usage: %[1]s deps show
`

type handler struct {
}

func New() command.Handler[any] {
	return &handler{}
}

func (c *handler) Handle(app command.App) (command.Action[any], []command.Option[any], error) {
	f := flag.NewFlagSet("", flag.ContinueOnError)
	err := f.Parse(app.Args())
	if err != nil {
		return nil, nil, err
	}

	if len(f.Args()) != 1 || f.Arg(0) != "show" {
		app.Usage(usage)
		return nil, nil, nil
	}

	return c, nil, nil
}

func (c *handler) Run(opts ...command.Option[any]) error {
	return deps.Run(opts...)
}
