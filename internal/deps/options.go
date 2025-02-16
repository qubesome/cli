package deps

import (
	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/types"
)

type Options struct {
	Config *types.Config
	Runner string
}

func WithConfig(cfg *types.Config) command.Option[Options] {
	return func(o *Options) {
		o.Config = cfg
	}
}

func WithRunner(runner string) command.Option[Options] {
	return func(o *Options) {
		o.Runner = runner
	}
}
