package log

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/stretchr/testify/assert"
)

func TestLogPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal("cannot get user home dir", err)
	}

	tests := []struct {
		name   string
		lookup func(key string) (string, bool)
		want   string
	}{
		{"default", os.LookupEnv, filepath.Join(home, ".local/state/qubesome/qubesome.log")},
		{"xdg_state_home override", func(key string) (string, bool) { return "/xdg/state", true }, "/xdg/state/qubesome/qubesome.log"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lookupEnv = tc.lookup

			got := logPath()
			Equal(t, tc.want, got)
		})
	}
}
