package gateway

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
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
			// Not "no gateway has been started". A gateway from an
			// older qubesome runs without writing this file, so what
			// can be said is that the log is not there to read.
			return fmt.Errorf("there is no gateway log at %s to read", path)
		}

		return fmt.Errorf("failed to open the gateway log %q: %w", path, err)
	}
	defer f.Close()

	selects := selector(opts.Profile, opts.Workload)

	// The tail is whatever the gateway was in the middle of writing. It
	// is not printed here and not thrown away either: a follow joins it
	// to the rest when the rest is written.
	tail, err := writeTail(w, f, opts.Last, selects)
	if err != nil {
		return err
	}

	if !opts.Follow {
		return nil
	}

	return follow(ctx, w, f, selects, tail)
}

// writeTail writes the lines of f that selects accepts: all of them, or
// only the final few when last is set. It returns the unterminated line
// the gateway was still writing, which it does not print.
//
// The file is left positioned at its end either way, which is where a
// follow carries on from.
func writeTail(w io.Writer, f *os.File, last int, selects func(string) bool) (string, error) {
	reader := bufio.NewReader(f)

	// A ring of the last lines wanted, so a log far larger than the
	// answer is not held in memory to produce it. It grows as lines are
	// read rather than being sized to last: that number is typed at a
	// terminal, and reserving it before a line has been read turns a
	// large enough -n into a way to bring qubesome down.
	//
	// next is where the oldest line sits once the ring is full, which is
	// also where the next one overwrites it. Nothing is shifted along.
	var ring []string
	var next int

	var tail string

	for {
		line, terminated, ok, err := readLine(reader)
		if err != nil {
			return "", fmt.Errorf("failed to read the gateway log: %w", err)
		}

		if !terminated {
			if ok {
				tail = line
			}

			break
		}

		// A line too long to hold was discarded as it was read, so there
		// is nothing to match a filter against or to print.
		if !ok || !selects(line) {
			continue
		}

		if last <= 0 {
			if err := writeLine(w, line); err != nil {
				return "", err
			}

			continue
		}

		if len(ring) < last {
			ring = append(ring, line)

			continue
		}

		ring[next] = line
		next = (next + 1) % last
	}

	for i := range ring {
		// next is 0 until the ring has filled, so this is the order the
		// lines were read in either way.
		if err := writeLine(w, ring[(next+i)%len(ring)]); err != nil {
			return "", err
		}
	}

	return tail, nil
}

// readLine returns the next line of r, without its newline.
//
// terminated is false when the line has no newline yet, which means the
// gateway is still writing it: what comes back is the part written so
// far, and the rest arrives on a later read.
//
// ok is false when the line was longer than maxLineLen. Such a line is
// discarded as it is read rather than returned, so a gateway writing
// without newlines cannot make a reader of its log hold the whole of it.
func readLine(r *bufio.Reader) (line string, terminated, ok bool, err error) {
	var b strings.Builder

	held := true

	for {
		// ReadSlice stops at the end of the buffer rather than growing
		// one, so what is read in a turn is bounded whatever the writer
		// is doing.
		chunk, err := r.ReadSlice('\n')

		if len(chunk) > 0 {
			if held && b.Len()+len(chunk) > maxLineLen {
				held = false

				b.Reset()
			}
			if held {
				b.Write(chunk)
			}
		}

		switch {
		case err == nil:
			return strings.TrimSuffix(b.String(), "\n"), true, held, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			return b.String(), false, held, nil
		default:
			return "", false, false, err
		}
	}
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
func follow(ctx context.Context, w io.Writer, f *os.File, selects func(string) bool, tail string) error {
	ticker := time.NewTicker(followInterval)
	defer ticker.Stop()

	reader := bufio.NewReader(f)

	var pending strings.Builder
	pending.WriteString(tail)

	// What the log had been grown to by the time the initial read
	// finished. A log shorter than this later is a different log: the
	// next gateway truncated this same path and started again.
	size, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return fmt.Errorf("failed to find the end of the gateway log: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			// The only way a follow ends is by being asked to stop, so
			// that is not an error to report.
			return nil
		case <-ticker.C:
			restarted, err := rewound(f, size)
			if err != nil {
				return err
			}
			if restarted {
				// Nothing of the old log is worth carrying over, least
				// of all half a line of it.
				reader.Reset(f)
				pending.Reset()
			}

			if err := followOnce(w, reader, &pending, selects); err != nil {
				return err
			}

			if size, err = f.Seek(0, io.SeekCurrent); err != nil {
				return fmt.Errorf("failed to find the end of the gateway log: %w", err)
			}
		}
	}
}

// rewound reports whether the log has been replaced by a shorter one, and
// puts f back at the start when it has.
//
// A gateway truncates the log it inherits rather than writing to a new
// path, so a follow that outlives one gateway is reading the next one's
// log through a descriptor still positioned at the end of the last one.
// Everything the new gateway said before it had said as much as the old
// one did would be stepped over.
func rewound(f *os.File, size int64) (bool, error) {
	fi, err := f.Stat()
	if err != nil {
		return false, fmt.Errorf("failed to look at the gateway log: %w", err)
	}

	if fi.Size() >= size {
		return false, nil
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return false, fmt.Errorf("failed to go back to the start of the gateway log: %w", err)
	}

	return true, nil
}

// followOnce drains what the reader can give without blocking, writing
// every whole line it completes. What is left over is kept in pending for
// the next tick, which is how a line still being written is not printed
// halfway.
func followOnce(w io.Writer, reader *bufio.Reader, pending *strings.Builder, selects func(string) bool) error {
	for {
		line, terminated, ok, err := readLine(reader)
		if err != nil {
			return fmt.Errorf("failed to read the gateway log: %w", err)
		}

		// A read that stopped short of a newline is a line the gateway
		// has not finished writing. It is held until it has.
		if !terminated {
			if !ok || pending.Len()+len(line) > maxLineLen {
				pending.Reset()

				return nil
			}
			pending.WriteString(line)

			return nil
		}

		if pending.Len() > 0 {
			line = pending.String() + line
			pending.Reset()
		}

		if !ok || !selects(line) {
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
	// O_APPEND as well as O_TRUNC. The uplink writes to this same file
	// through a descriptor of its own, and a write that is not an append
	// goes to wherever this descriptor's offset has reached, which is
	// behind whatever the uplink has added since. Both writers append, so
	// neither lands on the other.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|os.O_APPEND, files.FileMode)
	if err != nil {
		return nil, fmt.Errorf("failed to open the gateway log %q: %w", path, err)
	}

	return f, nil
}

// closeLog closes qubesome's own copy of the gateway log.
//
// The sandbox and the uplink are handed their own descriptors for it, so
// this one is closed as soon as they are started and closing it loses
// nothing: nothing here writes through it, so there is no buffered tail
// of a write to fail to reach the disk.
//
// It is still not discarded. A close that fails says the filesystem the
// log sits on is unwell, and that is worth knowing about the file a
// gateway explains itself in. It is a warning and not an error because
// the gateway it belongs to is already running by this point, and a log
// qubesome could not close is no reason to take one down.
func closeLog(f *os.File) {
	if err := f.Close(); err != nil {
		slog.Warn("failed to close the gateway log", "path", f.Name(), "error", err)
	}
}
