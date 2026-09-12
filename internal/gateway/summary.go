package gateway

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
)

// errNoLog reports a session with no gateway log to read. It is a distinct
// error because a caller diagnosing a host has to tell "the gateway said
// nothing worth reporting" from "there was nothing to read".
var errNoLog = errors.New("no gateway log")

// maxDeniedHosts bounds how many denied hosts a summary names. A policy
// that denies a great deal is working, and a diagnosis of it should say so
// in a line rather than reproduce the log.
const maxDeniedHosts = 5

// LogSummary is what a gateway log says has happened.
//
// It is deliberately small. It exists so that qubesome doctor can say
// whether the gateway has been refusing connections or failing to make
// them, without reading the log for the user or growing an opinion about
// what a policy ought to allow.
type LogSummary struct {
	// Decisions is how many connections the proxy classified.
	Decisions int

	// Denied is how many of those it refused. A denial is the policy
	// working, so this is not a count of faults. It is the answer to why
	// a workload could not reach something.
	Denied int

	// DeniedHosts names the distinct hosts that were denied, in the order
	// they were first refused, at most maxDeniedHosts of them.
	DeniedHosts []string

	// Errors is how many lines the gateway logged at error level. Unlike
	// a denial, each of these is something that did not work.
	Errors int

	// LastError is the message of the most recent of them.
	LastError string
}

// Summarise reads the gateway log at path and reports what it says.
//
// A line it cannot parse contributes nothing rather than failing the read.
// The log's shape belongs to the gateway, which is a separate component on
// its own release cycle, so a summary of it degrades to saying less rather
// than to refusing to say anything.
func Summarise(path string) (LogSummary, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return LogSummary{}, fmt.Errorf("%w at %s", errNoLog, path)
		}

		return LogSummary{}, fmt.Errorf("failed to open the gateway log %q: %w", path, err)
	}
	defer f.Close()

	var s LogSummary

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), maxLineLen)

	for scanner.Scan() {
		s.read(scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return LogSummary{}, fmt.Errorf("failed to read the gateway log %q: %w", path, err)
	}

	return s, nil
}

// read folds one log line into the summary.
func (s *LogSummary) read(line string) {
	if strings.EqualFold(logField(line, fieldLevel), "error") {
		s.Errors++
		s.LastError = logField(line, "msg")
	}

	action := logField(line, fieldAction)
	if action == "" {
		return
	}
	s.Decisions++

	if action != "deny" {
		return
	}
	s.Denied++

	host := logField(line, fieldHost)
	if host == "" || len(s.DeniedHosts) == maxDeniedHosts || slices.Contains(s.DeniedHosts, host) {
		return
	}
	s.DeniedHosts = append(s.DeniedHosts, host)
}
