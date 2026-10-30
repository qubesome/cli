package sandbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/qubesome/cli/internal/files"
)

// State records a running sandbox.
//
// The start time is recorded alongside the pid because pids are reused. A
// check on the pid alone would report a profile as running whenever an
// unrelated process happened to inherit its number.
type State struct {
	PID       int    `json:"pid"`
	StartTime uint64 `json:"startTime"`

	// Session is the user namespace the session holder was keeping open
	// when this was recorded, as the inode the kernel names it by. It is
	// zero for a sandbox that was recorded without one.
	//
	// It is here because a pid that is still alive does not make a
	// sandbox usable. A gateway outlives the launch that started it, and
	// when the holder goes and a later launch starts a fresh one the
	// surviving gateway's namespaces are owned by what is now a dead
	// sibling of the session. Nothing can be wired to it any more, and
	// nothing about the pid says so.
	Session uint64 `json:"session,omitempty"`

	// Address is the workload's address on the session gateway, and is
	// empty for a workload that has none.
	//
	// It is recorded because it is the other thing every later question
	// needs and nothing else writes it down. The gateway is told which
	// workload holds which address and qubesome tells it, but neither end
	// keeps a note a third process could read, so a diagnostic asking what
	// a running sandbox is on the gateway has only this record to go on.
	Address string `json:"address,omitempty"`

	// Gateway is the start time of the gateway process the sandbox was
	// wired to, and zero for one given no address.
	//
	// The session alone does not say which gateway that was. qubesome
	// gateway stop takes the gateway down while the holder stays, and the
	// next launch starts another under the same session with its own
	// addresses. A sandbox wired to the first is reachable from neither.
	Gateway uint64 `json:"gateway,omitempty"`
}

// Wiring is what a sandbox was wired to on the gateway. It is the zero
// value for a sandbox given no address.
type Wiring struct {
	Address string
	Session uint64
	Gateway uint64
}

// WriteState records a running sandbox at path.
func WriteState(path string, pid int) error {
	_, err := WriteStateWired(path, pid, Wiring{})
	return err
}

// WriteStateWired records a running sandbox and what it was wired to, and
// returns the record it wrote.
//
// The session and the gateway are recorded beside the address because the
// address alone does not say whose gateway it is on. A sandbox outlives
// the session it was wired under when the holder is killed, and the
// gateway it was wired to when that is stopped, and its record still names
// an address a later gateway knows nothing about.
//
// The record is returned so that whatever cleans up after the sandbox can
// tell it from one a later launch writes to the same path.
func WriteStateWired(path string, pid int, w Wiring) (State, error) {
	st, err := startTime(pid)
	if err != nil {
		return State{}, err
	}

	s := State{PID: pid, StartTime: st, Address: w.Address, Session: w.Session, Gateway: w.Gateway}

	return s, writeState(path, s)
}

// WriteStateSession records a running sandbox and the session user
// namespace it was started under.
//
// It is the gateway's writer. A workload sandbox is wired during its own
// launch, while the session that is wiring it is the one holding it, so
// there is nothing for it to disagree with later.
func WriteStateSession(path string, pid int, session uint64) error {
	st, err := startTime(pid)
	if err != nil {
		return err
	}

	return writeState(path, State{PID: pid, StartTime: st, Session: session})
}

func writeState(path string, s State) error {
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("failed to marshal sandbox state: %w", err)
	}

	if err := os.WriteFile(path, data, files.FileMode); err != nil {
		return fmt.Errorf("failed to write sandbox state %q: %w", path, err)
	}

	return nil
}

func unmarshalState(data []byte, s *State) error {
	if err := json.Unmarshal(data, s); err != nil {
		return fmt.Errorf("failed to parse sandbox state: %w", err)
	}
	return nil
}

// ReadState returns the sandbox recorded at path.
//
// It says what was written and not whether it is still true. A caller that
// needs the pid checks Alive first, since a state file outlives the
// process it names.
func ReadState(path string) (State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return State{}, fmt.Errorf("failed to read sandbox state %q: %w", path, err)
	}

	var s State
	if err := unmarshalState(data, &s); err != nil {
		return State{}, err
	}

	return s, nil
}

// RemoveStateOf removes the record at path if it is still the one own
// describes, which is the record a launch wrote, and reports whether it
// did.
//
// A record that names another process belongs to a launch that came after
// the one asking, and one that is gone was removed by whoever took the
// sandbox down. Neither is the asker's to remove, and neither is the
// gateway mapping under the same name. The start time is compared with
// the pid because a pid can be given to the next launch's sandbox as soon
// as the last one is reaped.
func RemoveStateOf(path string, own State) (bool, error) {
	s, err := ReadState(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	if s.PID != own.PID || s.StartTime != own.StartTime {
		return false, nil
	}

	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("failed to remove sandbox state %q: %w", path, err)
	}

	return true, nil
}

// Alive reports whether the sandbox recorded at path is still running.
//
// Anything unreadable, unparsable or mismatched reads as not running. A
// stale state file must not stop a profile from starting.
func Alive(path string) bool {
	s, err := ReadState(path)
	if err != nil {
		return false
	}

	st, err := startTime(s.PID)
	if err != nil {
		return false
	}

	return st == s.StartTime
}

// Exited reports whether the sandbox recorded at path has finished.
//
// It is not the negation of Alive, and the difference is a process that
// has been killed and not yet reaped. A zombie keeps its /proc entry and
// its start time still matches, so Alive reads it as running when it is
// only waiting to be collected. Nothing is being served by then, and
// something waiting for a sandbox to go would otherwise wait on a parent
// that may never come: the process that started a gateway is usually a
// qubesome run that exited at its terminal long ago.
//
// No record, or one naming a process /proc no longer has, is exited too.
// Both mean the same thing to a caller waiting for one to be gone.
func Exited(path string) bool {
	s, err := ReadState(path)
	if err != nil {
		return true
	}

	data, err := os.ReadFile(procStat(s.PID))
	if err != nil {
		return true
	}

	st, err := parseStartTime(string(data))
	if err != nil || st != s.StartTime {
		// A start time that no longer matches is a different process
		// wearing the same pid, so the recorded one has gone.
		return true
	}

	return parseZombie(string(data))
}

// parseZombie reports whether a stat line describes a process that has
// exited and is waiting to be reaped.
//
// Field 3 is the state character, and is the first after the command name
// for the reason parseStartTime counts from there.
func parseZombie(line string) bool {
	i := strings.LastIndex(line, ")")
	if i < 0 {
		return false
	}

	fields := strings.Fields(line[i+1:])

	return len(fields) > 0 && fields[0] == "Z"
}

func procStat(pid int) string {
	return "/proc/" + strconv.Itoa(pid) + "/stat"
}

// startTime returns the start time of a process in clock ticks since boot.
func startTime(pid int) (uint64, error) {
	path := procStat(pid)

	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("failed to read %q: %w", path, err)
	}

	return parseStartTime(string(data))
}

// ParentPID returns the pid of a process's parent.
func ParentPID(pid int) (int, error) {
	path := procStat(pid)

	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("failed to read %q: %w", path, err)
	}

	return parseParentPID(string(data))
}

// parseParentPID reads field 4 of a /proc/<pid>/stat line, counted from
// after the command name for the reason parseStartTime gives.
func parseParentPID(line string) (int, error) {
	i := strings.LastIndex(line, ")")
	if i < 0 {
		return 0, fmt.Errorf("malformed stat line: no command name")
	}

	// Field 3 is the first after the command name, so the parent is the
	// second of the remaining fields.
	const parentField = 2

	fields := strings.Fields(line[i+1:])
	if len(fields) < parentField {
		return 0, fmt.Errorf("malformed stat line: %d fields after the command name", len(fields))
	}

	ppid, err := strconv.Atoi(fields[parentField-1])
	if err != nil {
		return 0, fmt.Errorf("failed to parse parent pid: %w", err)
	}

	return ppid, nil
}

// parseStartTime reads field 22 of a /proc/<pid>/stat line.
//
// Field 2 is the command name in parentheses and may contain spaces and
// parentheses of its own, so the fields are counted from after the last
// closing parenthesis rather than from the start of the line.
func parseStartTime(line string) (uint64, error) {
	i := strings.LastIndex(line, ")")
	if i < 0 {
		return 0, fmt.Errorf("malformed stat line: no command name")
	}

	// Field 3 is the first after the command name, and the start time is
	// field 22, so it is the 20th of the remaining fields.
	const startTimeField = 20

	fields := strings.Fields(line[i+1:])
	if len(fields) < startTimeField {
		return 0, fmt.Errorf("malformed stat line: %d fields after the command name", len(fields))
	}

	st, err := strconv.ParseUint(fields[startTimeField-1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse process start time: %w", err)
	}

	return st, nil
}
