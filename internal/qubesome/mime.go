package qubesome

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"
)

func (q *Qubesome) HandleMime(in *WorkloadInfo, args []string, runnerOverride string) error {
	slog.Debug("handle mime", "profile", in, "args", args)

	if len(args) != 1 {
		return fmt.Errorf("incorrect usage: a single arg must be provided: %q", strings.Join(args, " "))
	}

	slog.Debug("debug", "config", in.Config)

	if in.Config == nil {
		return fmt.Errorf("missing qubesome config")
	}

	u, err := url.Parse(args[0])
	if err != nil {
		return fmt.Errorf("failed to parse mime %q: %w", args[0], err)
	}

	if u.Scheme == "" {
		slog.Debug("no scheme provided: falling back to default mime handler")
		if in.Config.DefaultMimeHandler == nil {
			return fmt.Errorf("cannot handle schemeless mime type: default mime handler is not set")
		}

		return q.runner(q.defaultWorkload(in, args), runnerOverride, false)
	}

	if m, ok := in.Config.MimeHandlers[u.Scheme]; ok {
		wi := WorkloadInfo{
			Name:    m.Workload,
			Profile: m.Profile,
			Args:    args,
			Config:  in.Config,
		}

		q.overrideWithProfile(in, &wi)
		return q.runner(wi, runnerOverride, false)
	}

	if in.Config.DefaultMimeHandler == nil {
		return fmt.Errorf("cannot handle mime type %q: the mime type is not configured nor is a default mime handler", u.Scheme)
	}

	slog.Debug("no scheme specific handler: falling back to default mime handler")

	// falls back to default
	return q.runner(q.defaultWorkload(in, args), runnerOverride, false)
}

func (q *Qubesome) overrideWithProfile(in *WorkloadInfo, wi *WorkloadInfo) {
	// If profile is set, it trumps the configuration.
	// This is to avoid cross-profile execution when running in
	// inception mode.
	if in != nil && in.Profile != "" {
		slog.Debug("overriding target profile",
			"old profile", wi.Profile, "new profile", in.Profile,
			"old path", wi.Path, "new path", in.Path)
		wi.Profile = in.Profile
		wi.Path = in.Path
	}
}

func (q *Qubesome) defaultWorkload(in *WorkloadInfo, args []string) WorkloadInfo {
	wi := WorkloadInfo{
		Name:    in.Config.DefaultMimeHandler.Workload,
		Profile: in.Config.DefaultMimeHandler.Profile,
		Args:    args,
		Config:  in.Config,
	}
	q.overrideWithProfile(in, &wi)
	return wi
}
