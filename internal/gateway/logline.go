package gateway

import "strings"

// The gateway writes its log with slog's text handler, so a line is a
// sequence of key=value pairs and a value holding a space is quoted. This
// reads that, tolerantly: a line it cannot make sense of yields nothing
// rather than an error, because the gateway's log format is the gateway's
// to change and a reader of it should degrade to showing the line as it
// is rather than refusing to show anything.
//
// The keys qubesome reads are the ones the gateway's audit line carries.
const (
	// fieldWorkload is the name qubesome registered the workload under.
	fieldWorkload = "workload"

	// fieldLevel is the slog level, which is how a line reporting a
	// malfunction is told from one reporting a decision.
	fieldLevel = "level"

	// fieldAction is the proxy's verdict: deny, splice or inject.
	fieldAction = "action"

	// fieldHost is the host a decision was about.
	fieldHost = "host"
)

// logField returns the value of key in a log line, and "" when the line
// does not carry it.
func logField(line, key string) string {
	// Anchored on a delimiter so that a key is not found inside another
	// one: "load" must not match "workload=".
	for i := 0; i+len(key)+1 <= len(line); i++ {
		if i > 0 && line[i-1] != ' ' {
			continue
		}
		if !strings.HasPrefix(line[i:], key+"=") {
			continue
		}

		return logValue(line[i+len(key)+1:])
	}

	return ""
}

// logValue reads one value from the start of rest, which is either quoted
// or runs to the next space.
func logValue(rest string) string {
	if strings.HasPrefix(rest, `"`) {
		// A quoted value ends at the next quote that is not escaped.
		// slog quotes a value holding a space, and escapes a quote
		// within it.
		var b strings.Builder
		for i := 1; i < len(rest); i++ {
			switch rest[i] {
			case '\\':
				if i+1 < len(rest) {
					i++
					b.WriteByte(rest[i])
				}
			case '"':
				return b.String()
			default:
				b.WriteByte(rest[i])
			}
		}

		return b.String()
	}

	if i := strings.IndexByte(rest, ' '); i >= 0 {
		return rest[:i]
	}

	return rest
}

// selector returns the test for whether a log line is about the workload
// being asked about.
//
// qubesome registers a workload with the gateway under its own name and
// its profile's, joined by a dash, and either half may hold a dash of its
// own. The pair therefore cannot be split back into two names with any
// certainty, so what is matched depends on how much was asked for:
//
//   - both named: the registered name is exactly the two joined, which is
//     the only unambiguous question of the three.
//   - a profile alone: the registered name ends with it.
//   - a workload alone: the registered name begins with it.
//
// A workload called "a-b" in profile "c" and one called "a" in profile
// "b-c" register the same name, and nothing here can tell them apart.
// Naming both halves is what avoids the question.
//
// A line naming no workload at all, which is what the gateway's own
// startup and shutdown lines look like, is not about the workload being
// asked about, so a filter drops it. Asking for no filter keeps
// everything.
func selector(profile, workload string) func(line string) bool {
	if profile == "" && workload == "" {
		return func(string) bool { return true }
	}

	switch {
	case profile != "" && workload != "":
		want := workload + "-" + profile

		return func(line string) bool { return logField(line, fieldWorkload) == want }

	case profile != "":
		suffix := "-" + profile

		return func(line string) bool {
			return strings.HasSuffix(logField(line, fieldWorkload), suffix)
		}

	default:
		prefix := workload + "-"

		return func(line string) bool {
			return strings.HasPrefix(logField(line, fieldWorkload), prefix)
		}
	}
}
