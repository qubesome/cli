package profiles

import (
	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/types"
)

type Options struct {
	GitUrl  string
	Path    string
	Profile string
	Config  *types.Config
}

func WithGitUrl(gitUrl string) command.Option[Options] {
	return func(o *Options) {
		o.GitUrl = gitUrl
	}
}

func WithPath(path string) command.Option[Options] {
	return func(o *Options) {
		o.Path = path
	}
}

func WithProfile(profile string) command.Option[Options] {
	return func(o *Options) {
		o.Profile = profile
	}
}
func WithConfig(config *types.Config) command.Option[Options] {
	return func(o *Options) {
		o.Config = config
	}
}
