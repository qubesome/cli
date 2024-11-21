package qubesome

import (
	"fmt"

	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/types"
)

type Options struct {
	Workload  string
	Config    *types.Config
	Profile   string
	Runner    string
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

func WithRunner(runner string) command.Option[Options] {
	return func(o *Options) {
		o.Runner = runner
	}
}

func WithWorkload(workload string) command.Option[Options] {
	return func(o *Options) {
		o.Workload = workload
	}
}

func WithConfig(cfg *types.Config) command.Option[Options] {
	return func(o *Options) {
		o.Config = cfg
	}
}

func (o *Options) Validate() error {
	if o.Config == nil {
		return fmt.Errorf("no config found")
	}
	if o.Workload == "" {
		return fmt.Errorf("missing workload name")
	}
	return nil
}
