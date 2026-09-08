// Package xkb carries the host's keyboard layout into a profile.
package xkb

import (
	"log/slog"
	"os"
	"regexp"
	"strings"

	"github.com/qubesome/cli/internal/files"
	"golang.org/x/sys/execabs"
)

// vars are the libxkbcommon environment variables, in the order
// setxkbmap reports the fields they correspond to.
var vars = []string{
	"XKB_DEFAULT_RULES",
	"XKB_DEFAULT_MODEL",
	"XKB_DEFAULT_LAYOUT",
	"XKB_DEFAULT_VARIANT",
	"XKB_DEFAULT_OPTIONS",
}

// fields are the setxkbmap -query keys matching vars, one for one.
var fields = []string{"rules", "model", "layout", "variant", "options"}

// value is what a keymap component may contain. Layouts and variants are
// comma separated lists of short tokens, and an option is a pair joined
// by a colon, so this is the union of those with nothing that could end
// an environment entry or start another.
var value = regexp.MustCompile(`^[a-zA-Z0-9_,:.+()-]*$`)

// Defaults returns the XKB_DEFAULT_ environment entries describing the
// host's keyboard layout, and nothing when it cannot be determined.
//
// A profile's compositor decides the keymap for everything inside it:
// Xwayland takes the compositor's, and every workload takes Xwayland's.
// libxkbcommon defaults to a US layout when nothing says otherwise, and
// weston starts with no config file, so without this a profile ignores
// the layout its user is typing on. The X server qubesome used before
// inherited the host keymap by being nested in the host's, which is why
// nothing had to carry it across until now.
func Defaults() []string {
	if env := fromEnv(); len(env) > 0 {
		return env
	}

	return fromSetxkbmap(query)
}

// fromEnv reads the variables a session may already have set. A Wayland
// session usually sets them, and a user who has set them by hand means
// them, so they win over asking the X server.
func fromEnv() []string {
	out := make([]string, 0, len(vars))

	for _, name := range vars {
		v, ok := os.LookupEnv(name)
		if !ok || v == "" {
			continue
		}
		if !value.MatchString(v) {
			slog.Warn("ignoring a keymap variable that is not a keymap", "name", name, "value", v)
			continue
		}

		out = append(out, name+"="+v)
	}

	// A layout alone is a keymap. Anything else without one is a model or
	// a set of options with nothing to apply them to.
	for _, e := range out {
		if strings.HasPrefix(e, "XKB_DEFAULT_LAYOUT=") {
			return out
		}
	}

	return nil
}

func query() ([]byte, error) {
	//nolint:gosec // G204: the binary is a fixed path and the argument is a literal.
	return execabs.Command(files.SetxkbmapBinary, "-query").Output()
}

// fromSetxkbmap parses setxkbmap -query, which prints one "key: value"
// per line and omits nothing, printing an empty value for a component
// that is not set.
func fromSetxkbmap(q func() ([]byte, error)) []string {
	out, err := q()
	if err != nil {
		slog.Debug("cannot read the host keyboard layout", "error", err)
		return nil
	}

	found := map[string]string{}
	for line := range strings.SplitSeq(string(out), "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}

		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if v == "" || !value.MatchString(v) {
			continue
		}

		found[k] = v
	}

	if found["layout"] == "" {
		return nil
	}

	env := make([]string, 0, len(vars))
	for i, f := range fields {
		if v := found[f]; v != "" {
			env = append(env, vars[i]+"="+v)
		}
	}

	return env
}
