// Package doctor diagnoses a qubesome installation.
//
// It answers two questions that the deps command does not. Whether the
// things qubesome needs are usable rather than merely installed, and why a
// particular profile or workload is not working.
package doctor

import (
	"fmt"
	"io"
	"strings"
)

// Status is the outcome of one check.
type Status int

const (
	// OK means the check passed and nothing needs doing.
	OK Status = iota

	// Warn means qubesome will run, but something is degraded or an
	// optional feature is unavailable.
	Warn

	// Fail means this is broken and something that depends on it will
	// not work.
	Fail
)

func (s Status) String() string {
	switch s {
	case OK:
		return "ok"
	case Warn:
		return "warn"
	case Fail:
		return "fail"
	default:
		return "unknown"
	}
}

// Check is one diagnosis.
//
// Detail says what was observed rather than repeating the check's name, so
// that a report is worth reading when everything passes. Fix says what to
// do about it, and is empty when there is nothing to do.
type Check struct {
	Name   string
	Status Status
	Detail string
	Fix    string
}

// Report is the result of a run, grouped into the sections it was
// gathered in.
type Report struct {
	Sections []Section
}

// Section groups the checks for one subject, such as the host or a named
// profile.
type Section struct {
	Title  string
	Checks []Check
}

// Add appends a section, skipping empty ones so that a report never shows
// a heading with nothing under it.
func (r *Report) Add(title string, checks []Check) {
	if len(checks) == 0 {
		return
	}

	r.Sections = append(r.Sections, Section{Title: title, Checks: checks})
}

// Failed reports whether anything is broken. It is what decides the
// command's exit status, so a warning does not count.
func (r *Report) Failed() bool {
	for _, s := range r.Sections {
		for _, c := range s.Checks {
			if c.Status == Fail {
				return true
			}
		}
	}

	return false
}

const (
	red   = "\033[31m"
	green = "\033[32m"
	amber = "\033[33m"
	dim   = "\033[2m"
	reset = "\033[0m"
)

func (s Status) marker(colour bool) string {
	text := map[Status]string{OK: " ok ", Warn: "warn", Fail: "FAIL"}[s]
	if !colour {
		return text
	}

	switch s {
	case OK:
		return green + text + reset
	case Warn:
		return amber + text + reset
	case Fail:
		return red + text + reset
	default:
		return text
	}
}

// Write renders the report.
//
// Fixes are indented under the check they belong to rather than collected
// at the end, because a reader who has found their failure should not then
// have to find its remedy somewhere else.
func (r *Report) Write(w io.Writer, colour bool) error {
	for i, s := range r.Sections {
		if i > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}

		if _, err := fmt.Fprintf(w, "%s\n", s.Title); err != nil {
			return err
		}

		for _, c := range s.Checks {
			if _, err := fmt.Fprintf(w, "  [%s] %s: %s\n",
				c.Status.marker(colour), c.Name, c.Detail); err != nil {
				return err
			}

			if c.Fix == "" {
				continue
			}

			for _, line := range strings.Split(c.Fix, "\n") {
				prefix := "         "
				if colour {
					prefix = dim + prefix
					line += reset
				}

				if _, err := fmt.Fprintf(w, "%s%s\n", prefix, line); err != nil {
					return err
				}
			}
		}
	}

	return nil
}
