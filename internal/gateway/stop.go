package gateway

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"syscall"
	"time"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/sandbox"
)

// Stop takes the session's gateway down and returns the pid it stopped, or
// zero when none was running.
//
// The session's namespace holder is left alone. A launch reuses whatever
// gateway it finds running, so a change to the gateway block reaches nothing
// until the running one is gone, and that is all this is for. Taking the
// holder down as well would make the next launch rebuild a user namespace
// that was never the problem, and every sandbox of the session nests inside
// that one.
//
// A session with no gateway in it is not an error to ask for. It is the
// state the caller wanted, and saying so is the answer rather than a
// failure.
func (g Gateway) Stop() (int, error) {
	// The lock file lives in the session directory, which a host that has
	// never started a session does not have.
	if err := os.MkdirAll(g.Dir, files.DirMode); err != nil {
		return 0, fmt.Errorf("failed to create the session dir %q: %w", g.Dir, err)
	}

	// The same lock startOnce takes, and for the same reason. A launch that
	// is between its own check and the record it writes would otherwise
	// have its gateway killed and its record kept, leaving the session
	// naming a process that is already gone.
	lock, err := acquire(g.LockPath)
	if err != nil {
		return 0, err
	}
	defer lock.Close()

	return g.stopLocked()
}

// stopLocked is Stop with the lock already held.
//
// It is apart from Stop because startOnce replaces a stranded gateway
// while holding that same lock, and flock is held per open file
// description: a second one taken from this process would wait for the
// first for as long as this process lives.
func (g Gateway) stopLocked() (int, error) {
	// sandbox.Alive and not a look at the pid. It compares the recorded
	// start time as well, so a record left behind by a gateway that crashed
	// reads as not running, and nothing here signals a pid the kernel has
	// since given to something else.
	if !sandbox.Alive(g.StatePath) {
		// Removing a record that already reads as not running changes no
		// answer. It is removed because reap would have removed it had the
		// launch that started the gateway still been there to do it.
		if err := os.Remove(g.StatePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return 0, fmt.Errorf("failed to remove the gateway state %q: %w", g.StatePath, err)
		}

		g.forgetConfig()

		return 0, nil
	}

	st, err := sandbox.ReadState(g.StatePath)
	if err != nil {
		return 0, err
	}

	// The recorded pid is the sandbox's own init and not the bwrap that
	// started it. Killing the outer bwrap does not signal the sandbox
	// nested below it. The init is pid 1 of its own pid namespace, and a
	// SIGKILL from an ancestor namespace takes the whole namespace with it,
	// which is the pid running.stop names when it takes down a gateway that
	// failed to come up.
	//
	// The uplink is not named here. pasta is not recorded anywhere, and the
	// tap it created lives in the network namespace that has just gone.
	if err := syscall.Kill(st.PID, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return 0, fmt.Errorf("failed to stop the gateway sandbox pid %d: %w", st.PID, err)
	}

	// A delivered signal is not a sandbox that has gone. The record has to
	// outlive the wait, because the next launch takes this same lock and
	// decides by what it finds: removing the record first would let it see
	// nothing, and start a replacement while the old pid namespace, its
	// outer bwrap and its uplink were still coming down.
	if err := waitGone(g.StatePath, stopGrace, stopPoll); err != nil {
		return st.PID, err
	}

	// A qubesome run at a terminal exits long before the gateway it started
	// does, so the goroutine that would have cleared this record has
	// usually gone with it.
	if err := os.Remove(g.StatePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return st.PID, fmt.Errorf("failed to remove the gateway state %q: %w", g.StatePath, err)
	}

	g.forgetConfig()

	return st.PID, nil
}

// forgetConfig drops the note of which config the gateway came from.
//
// It goes with the state file, because the two describe the same gateway
// and a record that outlived it would name the provenance of something
// that is no longer running. A failure is a warning: the next gateway to
// start overwrites this, so what is left behind is a stale path that only
// a status asked between the two would read, and saying so is better than
// failing a stop that otherwise worked.
func (g Gateway) forgetConfig() {
	if err := os.Remove(g.ConfigPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("failed to remove the gateway's config record",
			"path", g.ConfigPath, "error", err)
	}
}

const (
	// stopGrace bounds the wait for a signalled gateway to go. SIGKILL to
	// pid 1 of a pid namespace takes the namespace with it and is not
	// something the process can put off, so this is a ceiling on the
	// kernel finishing rather than an estimate of anything.
	stopGrace = 5 * time.Second

	// stopPoll is how often the record is checked while waiting. There is
	// nothing to wait on: the gateway is not this process's child, so it
	// cannot be reaped here and its going away is not something the
	// kernel will report.
	stopPoll = 20 * time.Millisecond
)

// waitGone blocks until the sandbox recorded at path has finished.
//
// sandbox.Exited and not the negation of sandbox.Alive, because a process
// killed by something that is not its parent stays in the table until
// somebody reaps it. Waiting for it to leave /proc would mean waiting for
// a parent that has usually exited long ago.
func waitGone(path string, grace, poll time.Duration) error {
	deadline := time.Now().Add(grace)

	for {
		if sandbox.Exited(path) {
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s waiting for the gateway sandbox to stop", grace)
		}

		time.Sleep(poll)
	}
}
