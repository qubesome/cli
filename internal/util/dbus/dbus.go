package dbus

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/qubesome/cli/internal/files"
	"golang.org/x/sys/execabs"
)

// Upstream documentation:
// https://specifications.freedesktop.org/notification-spec/latest/index.html
// https://linux.die.net/man/1/dbus-send

func dbusArgs(title, body string) []string {
	return []string{
		"--session",
		"--dest=org.freedesktop.Notifications",
		"--type=method_call",
		"--print-reply",
		"/org/freedesktop/Notifications",
		"org.freedesktop.Notifications.Notify",
		"string:qubesome",
		"uint32:0",
		"string:",
		"string:" + title,
		"string:" + body,
		"array:string:",
		"dict:string:string:",
		"int32:10000",
	}
}

func Notify(title, body string) error {
	args := dbusArgs(title, body)
	slog.Debug(files.DbusBinary, "args", args)

	//nolint
	cmd := execabs.Command(files.DbusBinary, args...)

	envVars := []string{
		"XDG_CONFIG_DIRS",
		"XDG_RUNTIME_DIR",
		"XDG_SEAT",
	}
	for _, v := range envVars {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", v, os.Getenv(v)))
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot run dbus-send: %w: %s", err, output)
	}

	return nil
}

func NotifyOrLog(title, body string) {
	err := Notify(title, body)
	if err != nil {
		slog.Error("cannot send notification", "error", err, "notification", body)
	}
}
