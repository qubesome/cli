package clipboard

import (
	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/types"
)

type Options struct {
	FromHost      bool
	ToHost        bool
	SourceProfile *types.Profile
	TargetProfile *types.Profile
	ContentType   string
}

func WithFromHost() command.Option[Options] {
	return func(o *Options) {
		o.FromHost = true
	}
}

func WithSourceProfile(p *types.Profile) command.Option[Options] {
	return func(o *Options) {
		o.SourceProfile = p
	}
}

func WithTargetProfile(p *types.Profile) command.Option[Options] {
	return func(o *Options) {
		o.TargetProfile = p
	}
}

func WithContentType(t string) command.Option[Options] {
	return func(o *Options) {
		o.ContentType = t
	}
}

func WithTargetHost() command.Option[Options] {
	return func(o *Options) {
		o.ToHost = true
	}
}
