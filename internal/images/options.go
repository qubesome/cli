package images

import (
	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/types"
)

type Options struct {
	Config *types.Config
}

func WithConfig(cfg *types.Config) command.Option[Options] {
	return func(o *Options) {
		o.Config = cfg
	}
}
