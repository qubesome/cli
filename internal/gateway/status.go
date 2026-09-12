package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/sandbox"
	"github.com/qubesome/cli/internal/session"
	"github.com/qubesome/cli/internal/types"
)

// statusReadyTimeout bounds the readiness question a status asks.
//
// It is not the client's own ten minutes. That deadline is sized for a
// launch, where Ready may be waiting on an image that is still being
// unpacked and where waiting is the whole job. A status is there to report,
// so a gateway that does not answer in a few seconds has to become a line in
// the report rather than a command that does not return. doctor bounds the
// same call for the same reason.
const statusReadyTimeout = 5 * time.Second

// Status is what qubesome knows about the session's gateway.
//
// Every field comes from something qubesome already holds: the config that
// names a gateway, the records a launch writes, and the readiness call the
// gateway has always served. Nothing here asks the gateway anything new, so
// a status needs no newer gateway than the one that is running.
type Status struct {
	// ConfigProblem says why Image, Policy and Subnet are empty. It is
	// empty when they are not. A status that cannot find a config says so,
	// rather than printing three blank lines that read as a gateway
	// configured with nothing.
	ConfigProblem string

	Image  string
	Policy string
	Subnet string

	// PolicyProblem says why the policy file path could not be resolved
	// against the directory the config was read from.
	PolicyProblem string

	// HolderPath and StatePath are the records that were read. They are
	// reported because they are the files an operator would otherwise have
	// to go looking for.
	HolderPath string
	StatePath  string

	HolderRunning bool
	HolderPID     int

	Running bool
	PID     int

	// ReadyErr is why a running gateway did not report itself ready. It is
	// empty both when it did and when there was no gateway to ask.
	ReadyErr string

	// GatewayAddr is the address the gateway holds on every veth. Allocated
	// is how many workload addresses have been handed out since it started,
	// and LastAddr is the highest of them.
	GatewayAddr string
	Allocated   uint64
	LastAddr    string

	// AddrProblem says why the addresses are not in the report.
	AddrProblem string

	// Wired are the workloads this session has given an address to, read
	// from the records their launches wrote. It is empty when none has.
	//
	// A workload that has since gone is listed as gone rather than
	// dropped. An address is never handed out twice within a session, so
	// what the record names is still true of the session even when the
	// sandbox it named is not there any more.
	Wired []WiredWorkload
}

// WiredWorkload is one workload the session has given an address to.
//
// It comes from the sandbox records the launches wrote and not from the
// gateway, for Inspect's reason: a status has to be able to answer for a
// gateway that has stopped answering.
type WiredWorkload struct {
	Profile string
	Name    string
	Address string

	// Runner distinguishes a machine from a sandbox. It is not in the
	// record, so it is empty unless a caller that knows it fills it in,
	// and an empty one is rendered as nothing rather than as a gap.
	Runner string

	Running bool
}

// Inspect gathers what is known about the session's gateway.
//
// cfg is the whole qubesome config rather than its gateway block, because a
// config that did not load and a config with no gateway block are different
// answers and a nil block cannot tell them apart. A bare status reaches a
// config only through a running profile or the user-level file, so it may
// well have neither, and reporting "no gateway is configured" for "no config
// was read" is how doctor once said there was no gateway on a host whose
// config configures one.
//
// ready is called only when a gateway is recorded as running. A readiness
// failure whose whole cause is that nothing is listening repeats the line
// above it.
func (g Gateway) Inspect(s session.Session, cfg *types.Config, ready func() error) Status {
	st := Status{
		HolderPath: s.StatePath,
		StatePath:  g.StatePath,
	}

	// sandbox.Alive and not a look at the pid. It compares the recorded
	// start time too, so a record left behind by a gateway that crashed
	// reads as not running rather than as whatever process has since been
	// given its number.
	st.HolderRunning = sandbox.Alive(s.StatePath)
	if st.HolderRunning {
		if rec, err := sandbox.ReadState(s.StatePath); err == nil {
			st.HolderPID = rec.PID
		}
	}

	st.Running = sandbox.Alive(g.StatePath)
	if st.Running {
		if rec, err := sandbox.ReadState(g.StatePath); err == nil {
			st.PID = rec.PID
		}
	}

	g.inspectConfig(&st, cfg)

	st.Wired = wiredWorkloads(cfg)

	if st.Running && ready != nil {
		if err := ready(); err != nil {
			st.ReadyErr = err.Error()
		}
	}

	return st
}

// wiredWorkloads reads every sandbox record that names a gateway address.
//
// The records are the source rather than the gateway, for Inspect's
// reason. A record with no address belongs to a workload that was launched
// without one, and it is left out: there is nothing about it a gateway
// status would say.
//
// Nothing here fails. A directory that cannot be read and a record that
// does not parse are both a workload this cannot report, and a status that
// refused to print because one file was malformed would be useless in
// exactly the situation it is run in.
func wiredWorkloads(cfg *types.Config) []WiredWorkload {
	if cfg == nil {
		return nil
	}

	profiles := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		profiles = append(profiles, name)
	}
	sort.Strings(profiles)

	var out []WiredWorkload

	for _, profile := range profiles {
		paths, err := filepath.Glob(filepath.Join(files.ProfileDir(profile), "sandbox-*.json"))
		if err != nil {
			continue
		}
		sort.Strings(paths)

		for _, path := range paths {
			rec, err := sandbox.ReadState(path)
			if err != nil || rec.Address == "" {
				continue
			}

			out = append(out, WiredWorkload{
				Profile: profile,
				Name:    workloadOfRecord(path),
				Address: rec.Address,
				Running: sandbox.Alive(path),
			})
		}
	}

	return out
}

// workloadOfRecord returns the workload a sandbox record belongs to. The
// name is in the filename, which is what StatePath built it from.
func workloadOfRecord(path string) string {
	return strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "sandbox-"), ".json")
}

// inspectConfig fills in what the config says a gateway should be, and the
// addresses handed out of the subnet it names.
func (g Gateway) inspectConfig(st *Status, cfg *types.Config) {
	if cfg == nil {
		st.ConfigProblem = "no qubesome config was loaded, so the image, policy and subnet it names are unknown"
		return
	}

	if cfg.Gateway == nil {
		st.ConfigProblem = "no gateway is configured, so workloads run with no egress"
		return
	}

	st.Image = cfg.Gateway.Image
	st.Subnet = cfg.Gateway.Subnet

	policy, err := cfg.Gateway.ConfigPath(cfg.RootDir)
	if err != nil {
		st.PolicyProblem = err.Error()
	} else {
		st.Policy = policy
	}

	g.inspectAddrs(st, cfg.Gateway)
}

// inspectAddrs reports the gateway's own address and how much of the subnet
// has been handed out.
//
// The count is read straight from the record rather than through readAlloc,
// which refuses a record naming a different subnet from the one asked for.
// That refusal is right for a launch, which must not hand out an address the
// running gateway knows nothing about. A status has to be able to report the
// mismatch instead of failing on it, since a config edited under a running
// gateway is one of the things it exists to show.
func (g Gateway) inspectAddrs(st *Status, cfg *types.GatewayConfig) {
	subnet, err := cfg.SubnetPrefix()
	if err != nil {
		st.AddrProblem = err.Error()
		return
	}

	addr, err := GatewayAddr(subnet)
	if err != nil {
		st.AddrProblem = err.Error()
		return
	}
	st.GatewayAddr = addr.String()

	a, err := readAllocFile(g.AllocPath)
	if err != nil {
		st.AddrProblem = err.Error()
		return
	}

	// What this describes is the record, not a gateway. The record
	// outlives the gateway that wrote it, and gateway stop leaves exactly
	// that behind: nothing running, and a note of the range the last one
	// handed addresses out of. A status naming a running gateway would be
	// false in the state it is most likely to be read in.
	if a.Subnet != "" && a.Subnet != subnet.String() {
		st.AddrProblem = fmt.Sprintf(
			"this session has handed addresses out of %s and the config now asks for %s, "+
				"so the session has to be restarted before the new range is used",
			a.Subnet, subnet)
		return
	}

	st.Allocated = a.Allocated
	if a.Allocated == 0 {
		return
	}

	// The first workload takes the address above the gateway's, and the
	// count only ever goes up, so the highest handed out is the one that
	// many places past it.
	last, err := addrAt(subnet, a.Allocated+1)
	if err != nil {
		st.AddrProblem = err.Error()
		return
	}
	st.LastAddr = last.String()
}

// readAllocFile returns the address record, or an empty one when a gateway
// has handed nothing out yet.
func readAllocFile(path string) (allocation, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return allocation{}, nil
	}
	if err != nil {
		return allocation{}, fmt.Errorf("failed to read the gateway addresses %q: %w", path, err)
	}

	var a allocation
	if err := json.Unmarshal(data, &a); err != nil {
		return allocation{}, fmt.Errorf("failed to parse the gateway addresses %q: %w", path, err)
	}

	return a, nil
}

// StatusReady asks the running gateway whether its resolver, proxy and
// netfilter ruleset are up, under a deadline a status command can wait out.
func (g Gateway) StatusReady() error {
	ctx, cancel := context.WithTimeout(context.Background(), statusReadyTimeout)
	defer cancel()

	c, err := g.Client()
	if err != nil {
		return err
	}

	return c.Ready(ctx)
}

// statusLine is one labelled fact.
type statusLine struct {
	label string
	text  string
}

// Write renders the status, one labelled line per fact.
func (s Status) Write(w io.Writer) error {
	for _, l := range s.lines() {
		if _, err := fmt.Fprintf(w, "%-10s %s\n", l.label, l.text); err != nil {
			return err
		}
	}

	return nil
}

// lines is the report in the order it is read in: what a gateway was asked
// to be, then whether there is one, then what it is doing.
func (s Status) lines() []statusLine {
	out := make([]statusLine, 0, 7)

	if s.ConfigProblem != "" {
		out = append(out, statusLine{"config", s.ConfigProblem})
	} else {
		policy := s.Policy
		if s.PolicyProblem != "" {
			policy = "cannot be resolved: " + s.PolicyProblem
		}

		out = append(out,
			statusLine{"image", s.Image},
			statusLine{"policy", policy},
			statusLine{"subnet", s.Subnet},
		)
	}

	out = append(out,
		statusLine{"holder", alive(s.HolderRunning, s.HolderPID, s.HolderPath)},
		statusLine{"gateway", alive(s.Running, s.PID, s.StatePath)},
	)

	if s.Running {
		out = append(out, statusLine{"readiness", s.readiness()})
	}

	if line, ok := s.addresses(); ok {
		out = append(out, statusLine{"addresses", line})
	}

	for _, w := range s.Wired {
		out = append(out, statusLine{"wired", w.line()})
	}

	return out
}

// line words one wired workload.
//
// The address comes straight after the name because it is the identity the
// gateway classifies by, and the runner comes last because it only
// distinguishes a machine from a sandbox.
func (w WiredWorkload) line() string {
	state := "gone"
	if w.Running {
		state = "running"
	}

	line := fmt.Sprintf("%s/%s at %s, %s", w.Profile, w.Name, w.Address, state)
	if w.Runner != "" {
		line += " (" + w.Runner + ")"
	}

	return line
}

// alive words the liveness of one recorded process. The record is named in
// both cases, because it is the file that answers the question and the one
// an operator reaches for next.
func alive(running bool, pid int, path string) string {
	if !running {
		return fmt.Sprintf("not running (no live record in %s)", path)
	}

	return fmt.Sprintf("running, pid %d (%s)", pid, path)
}

func (s Status) readiness() string {
	if s.ReadyErr != "" {
		return "not ready: " + s.ReadyErr
	}

	return "the resolver, proxy and ruleset are up"
}

// addresses words what the subnet has been used for, and reports whether
// there is anything to say. There is nothing without a subnet to say it of.
func (s Status) addresses() (string, bool) {
	if s.AddrProblem != "" {
		return s.AddrProblem, true
	}

	if s.GatewayAddr == "" {
		return "", false
	}

	// The count belongs to the gateway that is running. A record left by an
	// earlier one is removed by the next launch, so reporting it against no
	// gateway would name addresses nothing holds.
	if !s.Running || s.Allocated == 0 {
		return fmt.Sprintf("%s is the gateway's own", s.GatewayAddr), true
	}

	return fmt.Sprintf("%s is the gateway's own, %d handed out up to %s",
		s.GatewayAddr, s.Allocated, s.LastAddr), true
}
