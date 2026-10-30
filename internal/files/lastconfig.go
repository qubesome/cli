package files

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// LastConfigPath returns where the last config qubesome opened is
// recorded.
//
// It is in the qubesome directory and not the run directory, beside
// images-last-checked, because the point of it is to outlive the session
// that wrote it. A machine usually has one config, in a dotfiles
// repository somewhere the user does not want to retype, and the run
// directory only ever names the config of a profile that is running now.
func LastConfigPath() string {
	return filepath.Join(QubesomeDir(), "last-config")
}

// RememberConfig records the config qubesome has just opened.
//
// Failures are logged and swallowed. This is a convenience for the next
// invocation, so a home directory that cannot be written to is a reason
// to go without it rather than a reason to fail the command the user
// actually asked for.
//
// The path is written as it was resolved rather than as it was typed, so
// that a relative path does not become a different file the next time it
// is read from another directory.
func RememberConfig(path string) {
	abs, err := filepath.Abs(path)
	if err != nil {
		slog.Debug("cannot resolve the config path to remember it", "path", path, "error", err)

		return
	}

	if err := os.MkdirAll(QubesomeDir(), DirMode); err != nil {
		slog.Debug("cannot create the qubesome dir to remember the config", "error", err)

		return
	}

	if err := os.WriteFile(LastConfigPath(), []byte(abs+"\n"), FileMode); err != nil {
		slog.Debug("cannot remember the config", "path", abs, "error", err)
	}
}

// RememberedConfig returns the last config qubesome opened, if there is
// one and it is still there.
//
// A record naming a file that has gone is no answer at all: the
// repository was moved or removed, and offering it would send the user
// after a path that cannot work. It reads as nothing remembered, and the
// next config opened replaces it.
func RememberedConfig() (string, bool) {
	data, err := os.ReadFile(LastConfigPath())
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Debug("cannot read the remembered config", "error", err)
		}

		return "", false
	}

	path := strings.TrimSpace(string(data))
	if path == "" {
		return "", false
	}

	// The path is one qubesome wrote here itself, in a file under the
	// user's own home directory, and all that is done with it here is a
	// stat to decide whether it is worth offering. Nothing opens it
	// without the user being shown it and saying so.
	if _, err := os.Stat(path); err != nil { //nolint:gosec // G703: read back from a file this package wrote, and only offered to the user.
		slog.Debug("the remembered config is no longer there", "path", path, "error", err)

		return "", false
	}

	return path, true
}
