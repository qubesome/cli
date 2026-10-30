// Package workloads finds the workloads qubesome has started and takes
// them down.
//
// It reads the records the runners leave in each profile's run directory
// rather than asking anything that is running. A launch typed at a
// terminal returns as soon as its sandbox is up, so nothing is left
// holding a list, and the records are the one place every sandbox of
// every profile is written down.
package workloads

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/qubesome/cli/internal/sandbox"
)

const (
	// killGrace bounds the wait for a killed sandbox to be gone. A SIGKILL
	// is not refused, so this is the time the kernel takes to tear a pid
	// namespace down, and a sandbox still there after it is one the kill
	// did not reach.
	killGrace = 5 * time.Second

	killPoll = 20 * time.Millisecond
)

// ErrNotRecorded reports a workload that has no record in the profile asked
// about, or in any profile when none was.
var ErrNotRecorded = errors.New("no record of that workload")

// Record is one workload sandbox as its record describes it.
type Record struct {
	Profile  string
	Workload string

	// Name is the workload's name on the session gateway, the workload
	// and profile joined by a hyphen.
	Name string

	Path    string
	PID     int
	Address string

	// Running is false for a record left behind by a sandbox that has
	// gone, which is a record and not a workload.
	Running bool
}

// List reads every workload record under runDir, ordered by profile and
// then by workload.
//
// A record that cannot be read is left out rather than failing the list,
// because a list is wanted most when something has gone wrong.
func List(runDir string) ([]Record, error) {
	paths, err := filepath.Glob(filepath.Join(runDir, "*", "sandbox-*.json"))
	if err != nil {
		return nil, fmt.Errorf("failed to list workload records in %q: %w", runDir, err)
	}
	sort.Strings(paths)

	var out []Record
	for _, path := range paths {
		st, err := sandbox.ReadState(path)
		if err != nil {
			continue
		}

		profile := filepath.Base(filepath.Dir(path))
		name := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "sandbox-"), ".json")

		out = append(out, Record{
			Profile:  profile,
			Workload: strings.TrimSuffix(name, "-"+profile),
			Name:     name,
			Path:     path,
			PID:      st.PID,
			Address:  st.Address,
			Running:  sandbox.Alive(path),
		})
	}

	return out, nil
}

// Find returns the records of workload, in profile when one is named and
// in every profile otherwise. An empty workload matches every workload.
func Find(records []Record, workload, profile string) []Record {
	var out []Record
	for _, r := range records {
		if (workload == "" || r.Workload == workload) && (profile == "" || r.Profile == profile) {
			out = append(out, r)
		}
	}

	return out
}

// One returns the single record of workload, in profile when one is named.
//
// A workload of the same name in two profiles is two sandboxes, and which
// one was meant is not something to guess at when the next step is a kill.
func One(records []Record, workload, profile string) (Record, error) {
	found := Find(records, workload, profile)

	switch len(found) {
	case 0:
		return Record{}, fmt.Errorf("%w: %q", ErrNotRecorded, workload)
	case 1:
		return found[0], nil
	}

	profiles := make([]string, 0, len(found))
	for _, r := range found {
		profiles = append(profiles, r.Profile)
	}

	return Record{}, fmt.Errorf("workload %q is recorded in profiles %s: pick one with --profile",
		workload, strings.Join(profiles, ", "))
}

// Kill takes a workload's sandbox down and removes its record.
//
// It is a SIGKILL and not a request to stop. The process the record names
// for a sandbox is pid 1 of its own pid namespace, which the kernel shields
// from every signal it has no handler for, so a SIGTERM would usually be
// ignored. A SIGKILL from an ancestor namespace is the one signal it cannot
// refuse, and it takes the whole namespace with it.
//
// A record whose sandbox has already gone is only removed. That is the
// case worth having a command for: the record is what a second launch of a
// single instance workload looks for.
//
// The record is read again first. Between the list and the kill the
// workload may have been killed and started again by someone else, and the
// pid that was listed then names a process the record no longer vouches
// for. That is refused rather than signalled.
func Kill(r Record) error {
	st, err := sandbox.ReadState(r.Path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil
	case err != nil:
		return err
	case st.PID != r.PID:
		return fmt.Errorf("the workload recorded in %q was started again as pid %d since it was listed as pid %d",
			r.Path, st.PID, r.PID)
	}

	if sandbox.Alive(r.Path) {
		self, err := pidNS(os.Getpid())
		if err != nil {
			return err
		}

		for _, pid := range targets(r.PID, self, pidNS, childrenOf()) {
			if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
				return fmt.Errorf("failed to kill pid %d: %w", pid, err)
			}
		}

		if err := waitGone(r.Path); err != nil {
			return err
		}
	}

	// The record read above and not any record at the path. Once the
	// sandbox is gone a launch of the same workload can start and write
	// its own, which is not this kill's to remove.
	if _, err := sandbox.RemoveStateOf(r.Path, st); err != nil {
		return fmt.Errorf("failed to remove the workload record %q: %w", r.Path, err)
	}

	return nil
}

// targets returns the pids a SIGKILL has to reach to take down the sandbox
// a record names.
//
// The record names the sandbox's init when the launch was nested in the
// session, and the bwrap that started it when it was not. A bwrap without
// --die-with-parent leaves its sandbox running when it is killed, so for a
// pid in this process's own pid namespace the inits below it are found and
// killed first. Older records of a nested launch name its outer bwrap, which
// has a second bwrap between it and the init, and the walk goes through it.
//
// The recorded pid is always among them. With the init gone it is on its
// way out anyway, and for a machine run straight on the host it is the VMM
// itself.
func targets(pid int, self string, nsOf func(int) (string, error), children map[int][]int) []int {
	ns, err := nsOf(pid)
	if err != nil || ns != self {
		return []int{pid}
	}

	var out []int

	var walk func(int)
	walk = func(parent int) {
		for _, child := range children[parent] {
			ns, err := nsOf(child)
			if err != nil {
				continue
			}
			if ns != self {
				out = append(out, child)
				continue
			}
			walk(child)
		}
	}
	walk(pid)

	return append(out, pid)
}

// pidNS names the pid namespace a process is in.
func pidNS(pid int) (string, error) {
	ns, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/ns/pid")
	if err != nil {
		return "", fmt.Errorf("failed to read the pid namespace of %d: %w", pid, err)
	}

	return ns, nil
}

// childrenOf maps every process to its children, from one pass over /proc.
//
// /proc/<pid>/task/<tid>/children would answer for one process, but it is
// only there on a kernel built with CONFIG_PROC_CHILDREN. A process that
// exits during the pass is simply not in the map.
func childrenOf() map[int][]int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}

	out := make(map[int][]int)
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}

		ppid, err := sandbox.ParentPID(pid)
		if err != nil {
			continue
		}
		out[ppid] = append(out[ppid], pid)
	}

	return out
}

// waitGone blocks until the sandbox a record names is no longer running.
//
// sandbox.Exited and not sandbox.Alive, since only Exited reads a zombie as
// gone. A sandbox's parent is a bwrap, which reaps it, but one whose parent
// went first is reparented to whatever reaps orphans and may sit as a
// zombie for a moment.
func waitGone(path string) error {
	deadline := time.Now().Add(killGrace)
	for !sandbox.Exited(path) {
		if time.Now().After(deadline) {
			return fmt.Errorf("the workload recorded in %q is still running %s after it was killed", path, killGrace)
		}
		time.Sleep(killPoll)
	}

	return nil
}
