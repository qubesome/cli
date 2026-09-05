package qubesome

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"regexp"
	"strings"
)

// Schemes are alpha, followed by alphanumerics and a few punctuation
// characters, as per RFC 3986.
var mimeSchemeRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.\-]*$`)

func (q *Qubesome) HandleMime(in *WorkloadInfo, args []string, runnerOverride string) error {
	slog.Debug("handle mime", "profile", in, "args", args)

	if len(args) != 1 {
		return fmt.Errorf("incorrect usage: a single arg must be provided: %q", strings.Join(args, " "))
	}

	slog.Debug("debug", "config", in.Config)

	if in.Config == nil {
		return fmt.Errorf("missing qubesome config")
	}

	u, err := parseMimeArg(args[0])
	if err != nil {
		return err
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

// parseMimeArg parses the single argument xdg-open was called with.
//
// The argument reaches the target workload's command line, so it must
// name something to open and never look like a flag. It is either a URL
// with a scheme, or a path to a file that exists on the host.
func parseMimeArg(arg string) (*url.URL, error) {
	if arg == "" {
		return nil, fmt.Errorf("mime argument cannot be empty")
	}

	if strings.HasPrefix(arg, "-") {
		return nil, fmt.Errorf("mime argument %q cannot start with '-'", arg)
	}

	u, err := url.Parse(arg)
	if err != nil {
		return nil, fmt.Errorf("failed to parse mime %q: %w", arg, err)
	}

	if u.Scheme == "" {
		if _, err := os.Stat(arg); err != nil {
			// A stat that fails for any other reason, such as a
			// permission error, says nothing about whether the
			// argument names a file. Report what happened rather
			// than claiming it is not one.
			if !errors.Is(err, fs.ErrNotExist) {
				return nil, fmt.Errorf("failed to stat mime argument %q: %w", arg, err)
			}

			return nil, fmt.Errorf("mime argument %q is neither a URL nor an existing file", arg)
		}

		return u, nil
	}

	if !mimeSchemeRegex.MatchString(u.Scheme) {
		return nil, fmt.Errorf("mime argument %q has an invalid scheme", arg)
	}

	return u, nil
}
