package flatpak

import (
	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/types"
)

type Options struct {
	Name      string
	Config    *types.Config
	Profile   string
	ExtraArgs []string
}

func WithExtraArgs(args []string) command.Option[Options] {
	return func(o *Options) {
		o.ExtraArgs = args
	}
}

func WithProfile(profile string) command.Option[Options] {
	return func(o *Options) {
		o.Profile = profile
	}
}

func WithName(name string) command.Option[Options] {
	return func(o *Options) {
		o.Name = name
	}
}

func WithConfig(cfg *types.Config) command.Option[Options] {
	return func(o *Options) {
		o.Config = cfg
	}
}

func (o *Options) Validate() error {
	return types.ValidateFlatpakName(o.Name)
}
