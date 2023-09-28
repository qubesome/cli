package qubesome

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"
)

func (q *Qubesome) HandleMime(args []string) error {
	slog.Debug("handle mime", "args", args)

	if len(args) != 1 {
		return fmt.Errorf("incorrect usage: a single arg must be provided: %q", strings.Join(args, " "))
	}

	if q.Config == nil {
		return fmt.Errorf("missing qubesome config")
	}

	u, err := url.Parse(args[0])
	if err != nil {
		return fmt.Errorf("failed to parse mime %q: %w", args[0], err)
	}

	if u.Scheme == "" {
		if q.Config.DefaultMimeHandler == nil {
			return fmt.Errorf("cannot handle schemeless mime type: default mime handler is not set")
		}

		return q.runner(WorkloadInfo{
			Name:    q.Config.DefaultMimeHandler.Workload,
			Profile: q.Config.DefaultMimeHandler.Profile,
		})
	}

	if m, ok := q.Config.MimeHandlers[u.Scheme]; ok {
		return q.runner(WorkloadInfo{
			Name:    m.Workload,
			Profile: m.Profile,
		})
	}

	if q.Config.DefaultMimeHandler == nil {
		return fmt.Errorf("cannot handle mime type %q: the mime type is not configured nor is a default mime handler", u.Scheme)
	}

	// falls back to default
	return q.runner(WorkloadInfo{
		Name:    q.Config.DefaultMimeHandler.Workload,
		Profile: q.Config.DefaultMimeHandler.Profile,
	})
}
