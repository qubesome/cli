package gateway

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/qubesome/cli/internal/files"
)

// followInterval is how often a follow looks for more of the log.
//
// The gateway writes through a descriptor to a file and nothing notifies a
// reader of it, so this is a poll. It is short enough that a decision
// shows up while the workload that caused it is still on screen, and long
// enough that watching an idle gateway is not a busy loop.
const followInterval = 200 * time.Millisecond

// maxLineLen bounds one log line, in the initial read and while
// following. A gateway writing without newlines cannot make a reader of
// its log hold the whole of it in memory.
const maxLineLen = 1 << 20

// LogOptions selects what ShowLogs prints.
type LogOptions struct {
	// Path is the log to read. Empty means the session's own gateway log.
	Path string

	// Profile and Workload narrow the log to the lines about one
	// workload, one profile's workloads, or one workload wherever it
	// runs. See selector for what each combination matches, and why
	// naming both is the only exact question of the three.
	Profile  string
	Workload string

	// Last is how many of the log's final matching lines to print. Zero
	// prints all of them.
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
// therefore has nowhere to go that anybody could still be looking at,
// which is why it is written to a file and read back here.
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

	selects := selector(opts.Profile, opts.Workload)

	if err := writeTail(w, f, opts.Last, selects); err != nil {
		return err
	}

	if !opts.Follow {
		return nil
	}

	return follow(ctx, w, f, selects)
}

// writeTail writes the lines of f that selects accepts: all of them, or
// only the final few when last is set.
//
// The file is left positioned at its end either way, which is where a
// follow carries on from: the scanner stops having consumed everything up
// to EOF.
func writeTail(w io.Writer, f *os.File, last int, selects func(string) bool) error {
	// A ring of the last lines wanted, so a log far larger than the
	// answer is not held in memory to produce it. Zero means every line
	// is written as it is read and nothing is held at all.
	var ring []string
	if last > 0 {
		ring = make([]string, 0, last)
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), maxLineLen)

	for scanner.Scan() {
		line := scanner.Text()
		if !selects(line) {
			continue
		}

		if last <= 0 {
			if err := writeLine(w, line); err != nil {
				return err
			}

			continue
		}

		if len(ring) == last {
			ring = append(ring[:0], ring[1:]...)
		}
		ring = append(ring, line)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read the gateway log: %w", err)
	}

	for _, line := range ring {
		if err := writeLine(w, line); err != nil {
			return err
		}
	}

	return nil
}

func writeLine(w io.Writer, line string) error {
	if _, err := io.WriteString(w, line+"\n"); err != nil {
		return fmt.Errorf("failed to write the gateway log: %w", err)
	}

	return nil
}

// follow writes the lines appended to f that selects accepts, until ctx is
// done.
//
// Only whole lines are written. A line is what carries the workload a
// decision was about, so half of one cannot be matched against a filter,
// and printing it unmatched would show another workload's log to someone
// who asked not to see it.
func follow(ctx context.Context, w io.Writer, f *os.File, selects func(string) bool) error {
	ticker := time.NewTicker(followInterval)
	defer ticker.Stop()

	reader := bufio.NewReader(f)
	var pending strings.Builder

	for {
		select {
		case <-ctx.Done():
			// The only way a follow ends is by being asked to stop, so
			// that is not an error to report.
			return nil
		case <-ticker.C:
			if err := followOnce(w, reader, &pending, selects); err != nil {
				return err
			}
		}
	}
}

// followOnce drains what the reader can give without blocking, writing
// every whole line it completes. What is left over is kept in pending for
// the next tick, which is how a line still being written is not printed
// halfway.
func followOnce(w io.Writer, reader *bufio.Reader, pending *strings.Builder, selects func(string) bool) error {
	for {
		chunk, err := reader.ReadString('\n')

		// A read that stopped short of a newline is a line the gateway
		// has not finished writing. It is held until it has.
		if errors.Is(err, io.EOF) {
			if pending.Len()+len(chunk) > maxLineLen {
				pending.Reset()

				return nil
			}
			pending.WriteString(chunk)

			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to read the gateway log: %w", err)
		}

		line := strings.TrimSuffix(chunk, "\n")
		if pending.Len() > 0 {
			line = pending.String() + line
			pending.Reset()
		}

		if !selects(line) {
			continue
		}
		if err := writeLine(w, line); err != nil {
			return err
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
