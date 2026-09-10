package gateway

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const decision = `time=2026-09-10T14:22:32.892Z level=INFO msg="proxy decision" ` +
	`plane=proxy workload=cli-llm-work host=api.anthropic.com action=splice ` +
	`reason="tunnelling TLS untouched"`

func TestLogField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line string
		key  string
		want string
	}{
		{"a bare value", decision, "workload", "cli-llm-work"},
		{"the first field", decision, "time", "2026-09-10T14:22:32.892Z"},
		{"a quoted value", decision, "msg", "proxy decision"},
		{"the last field", decision, "reason", "tunnelling TLS untouched"},
		{"a key that is absent", decision, "profile", ""},
		{"a key that is only a suffix of another", decision, "load", ""},
		{"an empty line", "", "workload", ""},
		{"a value at the end with no newline", "level=INFO", "level", "INFO"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, logField(tc.line, tc.key))
		})
	}
}

// The gateway knows a workload by the name qubesome registered, which is
// the workload's own name and its profile's joined by a dash. Both halves
// may hold a dash themselves, so the pair cannot be split back apart with
// any certainty. Naming both is therefore the exact question, and naming
// one alone is a prefix or a suffix of it.
func TestSelects(t *testing.T) {
	t.Parallel()

	line := func(workload string) string {
		return `level=INFO msg="proxy decision" workload=` + workload + ` action=splice`
	}

	tests := []struct {
		name     string
		profile  string
		workload string
		line     string
		want     bool
	}{
		{"no filter takes everything", "", "", line("cli-llm-work"), true},
		{"no filter takes a line naming no workload", "", "", "msg=\"starting gateway\"", true},

		{"both, matching", "work", "cli-llm", line("cli-llm-work"), true},
		{"both, wrong profile", "personal", "cli-llm", line("cli-llm-work"), false},
		{"both, wrong workload", "work", "chrome", line("cli-llm-work"), false},
		{"both, matching only as a prefix", "work", "cli", line("cli-llm-work"), false},

		{"profile only", "work", "", line("cli-llm-work"), true},
		{"profile only, another profile", "personal", "", line("cli-llm-work"), false},
		{"profile only, the whole name", "cli-llm-work", "", line("cli-llm-work"), false},

		{"workload only", "", "cli-llm", line("cli-llm-work"), true},
		{"workload only, another workload", "", "chrome", line("cli-llm-work"), false},
		{"workload only, the whole name", "", "cli-llm-work", line("cli-llm-work"), false},

		{"a filtered line naming no workload is not about it", "work", "", "msg=\"starting gateway\"", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sel := selector(tc.profile, tc.workload)
			assert.Equal(t, tc.want, sel(tc.line))
		})
	}
}
