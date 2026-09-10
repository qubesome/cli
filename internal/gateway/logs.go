package gateway

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/qubesome/cli/internal/files"
)

// followInterval is how often a follow looks for more of the log.
//
// The gateway writes through a pipe to a file and nothing notifies a reader
// of it, so this is a poll. It is short enough that a decision shows up
// while the workload that caused it is still on screen, and long enough
// that watching an idle gateway is not a busy loop.
const followInterval = 200 * time.Millisecond

// followChunk bounds a single read while following, so one write cannot
// make the reader hold the whole of a chatty log in memory.
const followChunk = 32 * 1024

// LogOptions selects what ShowLogs prints.
type LogOptions struct {
	// Path is the log to read. Empty means the session's own gateway log.
	Path string

	// Last is how many of the log's final lines to print. Zero prints all
	// of it.
	Last int

	// Follow keeps printing what is appended, until the context is done.
	Follow bool
}

func (o LogOptions) path() string {
	if o.Path != "" {
		return o.Path
	}

	return files.GatewayLogPath()
}

// ShowLogs writes the session gateway's log to w.
//
// The gateway is started by whichever qubesome run found none already
// running, and it is put in a session of its own so that a Ctrl-C at that
// terminal does not take the session's egress away with it. Its output
// therefore has nowhere to go that anybody could still be looking at, which
// is why it is written to a file and read back here.
func ShowLogs(ctx context.Context, w io.Writer, opts LogOptions) error {
	path := opts.path()

	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("there is no gateway log at %s: this session has not started a gateway", path)
		}

		return fmt.Errorf("failed to open the gateway log %q: %w", path, err)
	}
	defer f.Close()

	if err := writeTail(w, f, opts.Last); err != nil {
		return err
	}

	if !opts.Follow {
		return nil
	}

	return follow(ctx, w, f)
}

// writeTail copies the log to w, from the start or from the last lines of
// it. The file is left positioned at its end either way, which is where a
// follow carries on from.
func writeTail(w io.Writer, f *os.File, last int) error {
	if last <= 0 {
		if _, err := io.Copy(w, f); err != nil {
			return fmt.Errorf("failed to read the gateway log: %w", err)
		}

		return nil
	}

	// Read the whole file to find where its last lines begin. A gateway log
	// is bounded by the life of one gateway rather than of the session, so
	// this is a file a terminal was going to be shown anyway. Seeking
	// backwards in chunks would be the answer if that stopped being true.
	body, err := io.ReadAll(f)
	if err != nil {
		return fmt.Errorf("failed to read the gateway log: %w", err)
	}

	if _, err := w.Write(tail(body, last)); err != nil {
		return fmt.Errorf("failed to write the gateway log: %w", err)
	}

	return nil
}

// tail returns the last n lines of body.
func tail(body []byte, n int) []byte {
	// A trailing newline ends the last line rather than starting another,
	// so it is not one of the separators being counted back through.
	end := len(bytes.TrimSuffix(body, []byte("\n")))

	for range n {
		i := bytes.LastIndexByte(body[:end], '\n')
		if i < 0 {
			return body
		}
		end = i
	}

	return body[end+1:]
}

// follow prints what is appended to f until ctx is done.
func follow(ctx context.Context, w io.Writer, f *os.File) error {
	ticker := time.NewTicker(followInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// The only way a follow ends is by being asked to stop, so
			// that is not an error to report.
			return nil
		case <-ticker.C:
			// Copied in bounded steps rather than to EOF in one call, so a
			// gateway writing faster than this reads cannot keep it here.
			if _, err := io.CopyN(w, f, followChunk); err != nil && !errors.Is(err, io.EOF) {
				return fmt.Errorf("failed to read the gateway log: %w", err)
			}
		}
	}
}

// appendLog opens the gateway's log to add to what is already there.
//
// It is what the uplink writes through. The uplink is started once the
// gateway sandbox is up, so the log it joins is the one that sandbox's
// launch has already begun, and truncating here would throw away the lines
// explaining a gateway that had trouble coming up.
func appendLog(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, files.FileMode)
	if err != nil {
		return nil, fmt.Errorf("failed to open the gateway log %q: %w", path, err)
	}

	return f, nil
}

// openLog opens the gateway's log for the sandbox to write to.
//
// It is truncated rather than appended to. The log is the record of the
// gateway that is running, and one gateway replaces another only by the
// first having gone, so carrying the old one's lines forward would only
// make it harder to tell which of them explains what is happening now.
//
// 0600 because a gateway log holds every name a workload asked for and
// every host it was allowed or refused, which is a record of what the user
// was doing.
func openLog(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, files.FileMode)
	if err != nil {
		return nil, fmt.Errorf("failed to open the gateway log %q: %w", path, err)
	}

	return f, nil
}
