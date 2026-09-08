package profiles

import (
	"github.com/qubesome/cli/internal/command"
)

type Options struct {
	GitURL      string
	Path        string
	Local       string
	Profile     string
	Interactive bool
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

func WithLocal(local string) command.Option[Options] {
	return func(o *Options) {
		o.Local = local
	}
}

func WithProfile(profile string) command.Option[Options] {
	return func(o *Options) {
		o.Profile = profile
	}
}

func WithInteractive() command.Option[Options] {
	return func(o *Options) {
		o.Interactive = true
	}
}
