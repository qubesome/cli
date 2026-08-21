package types

import (
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// DbusMode selects how a workload reaches dbus.
type DbusMode string

const (
	// DbusNone isolates the workload with a per-profile dbus session.
	DbusNone DbusMode = "none"
	// DbusFiltered exposes the host bus through an xdg-dbus-proxy that
	// only allows the configured rules.
	DbusFiltered DbusMode = "filtered"
	// DbusFull exposes the raw host bus (session and system).
	DbusFull DbusMode = "full"
)

// DbusPolicy defines dbus access for a profile or workload.
//
// Rules are xdg-dbus-proxy style strings: "<verb>=<name>[=<method-rule>]",
// where verb is one of see, talk, own, call, broadcast. Names accept the
// trailing ".*" subtree wildcard.
type DbusPolicy struct {
	Mode    DbusMode `yaml:"mode"`
	Session []string `yaml:"session"`
	System  []string `yaml:"system"`
}

// UnmarshalYAML accepts either a bare bool (legacy) or the struct form.
// true maps to full, false maps to none.
func (d *DbusPolicy) UnmarshalYAML(value *yaml.Node) error {
	var b bool
	if err := value.Decode(&b); err == nil {
		if b {
			d.Mode = DbusFull
		} else {
			d.Mode = DbusNone
		}
		return nil
	}

	type raw DbusPolicy
	var r raw
	if err := value.Decode(&r); err != nil {
		return err
	}
	*d = DbusPolicy(r)
	if d.Mode == "" {
		d.Mode = DbusNone
	}
	return nil
}

type dbusRule struct {
	verb   string // see, talk, own, call, broadcast
	name   string // bus name pattern
	method string // method-rule for call/broadcast, empty otherwise
}

var dbusVerbs = map[string]bool{
	"see": true, "talk": true, "own": true, "call": true, "broadcast": true,
}

func parseDbusRule(s string) (dbusRule, error) {
	parts := strings.SplitN(s, "=", 3)
	if len(parts) < 2 {
		return dbusRule{}, fmt.Errorf("dbus rule %q: expected <verb>=<name>", s)
	}
	verb, name := parts[0], parts[1]
	if !dbusVerbs[verb] {
		return dbusRule{}, fmt.Errorf("dbus rule %q: unknown verb %q", s, verb)
	}
	if name == "" {
		return dbusRule{}, fmt.Errorf("dbus rule %q: empty name", s)
	}

	needsMethod := verb == "call" || verb == "broadcast"
	var method string
	if len(parts) == 3 {
		method = parts[2]
	}
	if needsMethod && method == "" {
		return dbusRule{}, fmt.Errorf("dbus rule %q: %s requires a method-rule", s, verb)
	}
	if !needsMethod && len(parts) == 3 {
		return dbusRule{}, fmt.Errorf("dbus rule %q: %s does not take a method-rule", s, verb)
	}
	return dbusRule{verb: verb, name: name, method: method}, nil
}

// Validate checks the mode and, regardless of mode, that every rule string
// is well formed. Rules are validated even for non-filtered modes so a typo
// is caught rather than silently ignored. An empty mode string is treated as
// DbusNone to accept the Go zero value without requiring explicit initialization.
func (d DbusPolicy) Validate() error {
	switch d.Mode {
	case "", DbusNone, DbusFiltered, DbusFull:
	default:
		return fmt.Errorf("dbus: unknown mode %q", d.Mode)
	}
	for _, r := range d.Session {
		if _, err := parseDbusRule(r); err != nil {
			return fmt.Errorf("dbus session: %w", err)
		}
	}
	for _, r := range d.System {
		if _, err := parseDbusRule(r); err != nil {
			return fmt.Errorf("dbus system: %w", err)
		}
	}
	return nil
}

// nameRank orders see < talk < own for the see/talk/own hierarchy.
func nameRank(verb string) int {
	switch verb {
	case "see":
		return 1
	case "talk":
		return 2
	case "own":
		return 3
	default:
		return 0
	}
}

// patternCovers reports whether pattern covers target. Supports exact match
// and a trailing ".*" subtree wildcard (e.g. "org.foo.*" covers "org.foo" and
// "org.foo.Bar").
func patternCovers(pattern, target string) bool {
	if pattern == target {
		return true
	}
	if strings.HasSuffix(pattern, ".*") {
		base := strings.TrimSuffix(pattern, ".*")
		if target == base {
			return true
		}
		if strings.HasPrefix(target, base+".") {
			return true
		}
	}
	return false
}

// ruleCovers reports whether the ceiling rule permits everything the want rule
// asks for.
func ruleCovers(ceiling, want dbusRule) bool {
	if !patternCovers(ceiling.name, want.name) {
		return false
	}

	switch want.verb {
	case "see", "talk", "own":
		return nameRank(ceiling.verb) >= nameRank(want.verb)
	case "call", "broadcast":
		// talk (or own) implies the ability to call/broadcast.
		if nameRank(ceiling.verb) >= nameRank("talk") {
			return true
		}
		if ceiling.verb == want.verb {
			return patternCovers(ceiling.method, want.method)
		}
		return false
	default:
		return false
	}
}

func modeRank(m DbusMode) int {
	switch m {
	case DbusFull:
		return 2
	case DbusFiltered:
		return 1
	default:
		return 0
	}
}

func minMode(a, b DbusMode) DbusMode {
	if modeRank(a) <= modeRank(b) {
		return a
	}
	return b
}

// intersectRules keeps each want rule only if some ceiling rule covers it.
// Invalid rules are skipped. Policy validation catches them earlier.
func intersectRules(want, ceiling []string) []string {
	var out []string
	for _, ws := range want {
		wr, err := parseDbusRule(ws)
		if err != nil {
			continue
		}
		for _, cs := range ceiling {
			cr, err := parseDbusRule(cs)
			if err != nil {
				continue
			}
			if ruleCovers(cr, wr) {
				out = append(out, ws)
				break
			}
		}
	}
	return out
}

// MergeDbus combines a workload policy with its profile policy. The profile is
// the ceiling: effective mode is the lower of the two, and filtered rules are
// bounded by the profile.
func MergeDbus(workload, profile DbusPolicy) DbusPolicy {
	switch minMode(workload.Mode, profile.Mode) {
	case DbusFull:
		return DbusPolicy{Mode: DbusFull}
	case DbusFiltered:
		// Handled below.
	default:
		// None or any unknown mode fails closed to an isolated bus.
		return DbusPolicy{Mode: DbusNone}
	}

	// Effective mode is filtered.
	switch {
	case profile.Mode == DbusFull:
		return DbusPolicy{Mode: DbusFiltered, Session: workload.Session, System: workload.System}
	case workload.Mode == DbusFull:
		return DbusPolicy{Mode: DbusFiltered, Session: profile.Session, System: profile.System}
	default:
		return DbusPolicy{
			Mode:    DbusFiltered,
			Session: intersectRules(workload.Session, profile.Session),
			System:  intersectRules(workload.System, profile.System),
		}
	}
}

// canonMode treats an empty mode as DbusNone so an unset policy is comparable
// to an explicit none.
func canonMode(m DbusMode) DbusMode {
	if m == "" {
		return DbusNone
	}
	return m
}

// DbusEqual reports whether two policies grant the same access. An unset policy
// (empty mode) compares equal to an explicit none, so a workload that omits
// dbus is not reported as differing from its isolated effective policy.
func DbusEqual(a, b DbusPolicy) bool {
	return canonMode(a.Mode) == canonMode(b.Mode) &&
		slices.Equal(a.Session, b.Session) &&
		slices.Equal(a.System, b.System)
}

// ProxyFlags renders rule strings into xdg-dbus-proxy flags. It always starts
// with --filter so the proxy denies everything not explicitly allowed.
func ProxyFlags(rules []string) []string {
	out := make([]string, 0, len(rules)+1)
	out = append(out, "--filter")
	for _, r := range rules {
		out = append(out, "--"+r)
	}
	return out
}
