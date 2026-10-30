package gateway

import (
	"errors"
	"fmt"

	"github.com/qubesome/cli/internal/sandbox"
)

// identity names the session and the gateway now running in it: the
// session's user namespace and the gateway's start time.
func (g Gateway) identity() (session, instance uint64, err error) {
	session, err = g.Session.UsernsID()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to identify the session the gateway is in: %w", err)
	}

	if !sandbox.Alive(g.StatePath) {
		return 0, 0, errors.New("the session gateway is not running")
	}

	rec, err := sandbox.ReadState(g.StatePath)
	if err != nil {
		return 0, 0, err
	}

	return session, rec.StartTime, nil
}

// wiredTo reports whether a sandbox record is of a workload wired to the
// gateway session and instance name. A record with any part missing is
// not, and neither is one asked about when no gateway is running.
func wiredTo(rec sandbox.State, session, instance uint64) bool {
	return rec.Address != "" && session != 0 && instance != 0 &&
		rec.Session == session && rec.Gateway == instance
}

// Outlived refuses a sandbox recorded at path that is still running wired
// to a gateway that is no longer the session's.
//
// That is a gateway of a session that has ended, or one stopped with
// qubesome gateway stop and since replaced. Nothing can wire the sandbox
// to the gateway now running while it runs. Handing it a second launch, or
// reporting it as already up, would leave a workload with no egress and
// nothing saying why. A record given no address depends on no gateway and
// is never refused.
func (g Gateway) Outlived(path, name string) error {
	// An unreadable record is one Alive has already read as not running,
	// so it is treated as one given no address.
	var rec sandbox.State
	if r, err := sandbox.ReadState(path); err == nil {
		rec = r
	}
	if rec.Address == "" {
		return nil
	}

	// A failure leaves both at zero, which wiredTo reads as no gateway to
	// be wired to.
	session, instance, _ := g.identity()
	if wiredTo(rec, session, instance) {
		return nil
	}

	return fmt.Errorf("%s is still running wired to a gateway that has since gone, so it has no egress: stop it and launch it again", name)
}
