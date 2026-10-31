package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

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
//
// The one exception is a record that names the session and no gateway.
// That is every record a release from before the gateway was named
// wrote, and it is taken to be wired to the gateway running in its
// session, as that release took it. Reading it as wired to a gateway
// that has gone would refuse every workload running across an upgrade,
// and leave its name mapped once it was killed.
func wiredTo(rec sandbox.State, session, instance uint64) bool {
	if rec.Address == "" || session == 0 || instance == 0 || rec.Session != session {
		return false
	}

	return rec.Gateway == instance || rec.Gateway == 0
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
	// A record that names neither is from a release that recorded no
	// wiring at all. Those were never refused, and nothing in the record
	// says they should be now.
	if rec.Address == "" || (rec.Session == 0 && rec.Gateway == 0) {
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

// Unregister drops the mapping of name to the address w records, if the
// gateway running now is the one w was wired to.
//
// A gateway that is restarted counts addresses from the start again, so a
// workload of the new one can come to hold the name and the address a
// workload of the old one held. A cleanup of the old one arriving after
// that would remove the new one's mapping, so it reaches only the gateway
// it was wired to. The lock is the one a start and a stop take, held from
// the check to the call, so the gateway cannot be replaced in between.
//
// It is best effort, for the reason Attach.Unregister gives.
func (g Gateway) Unregister(ctx context.Context, w sandbox.Wiring, name string) {
	if err := g.unregister(ctx, w, name); err != nil {
		slog.Debug("failed to unregister a workload", "workload", name, "error", err)
	}
}

func (g Gateway) unregister(ctx context.Context, w sandbox.Wiring, name string) error {
	lock, err := acquire(g.LockPath)
	if err != nil {
		return err
	}
	defer lock.Close()

	session, instance, err := g.identity()
	if err != nil {
		return err
	}

	rec := sandbox.State{Address: w.Address, Session: w.Session, Gateway: w.Gateway}
	if !wiredTo(rec, session, instance) {
		slog.Debug("not unregistering a workload wired to a gateway that has gone", "workload", name)
		return nil
	}

	c, err := g.Client()
	if err != nil {
		return err
	}

	return c.Unregister(ctx, name, w.Address)
}
