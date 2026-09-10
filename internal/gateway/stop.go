package gateway

import (
	"errors"
	"fmt"
	"os"
	"syscall"

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

	// A qubesome run at a terminal exits long before the gateway it started
	// does, so the goroutine that would have cleared this record has
	// usually gone with it.
	if err := os.Remove(g.StatePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return st.PID, fmt.Errorf("failed to remove the gateway state %q: %w", g.StatePath, err)
	}

	return st.PID, nil
}
