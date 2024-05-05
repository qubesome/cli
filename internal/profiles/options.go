package profiles

import (
	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/types"
)

type Options struct {
	GitURL  string
	Path    string
	Profile string
	Config  *types.Config
}

func WithGitURL(gitURL string) command.Option[Options] {
	return func(o *Options) {
		o.GitURL = gitURL
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
