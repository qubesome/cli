package dbus

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/godbus/dbus/v5"
)

// Upstream documentation:
// https://specifications.freedesktop.org/notification-spec/latest/index.html

const (
	notificationsName = "org.freedesktop.Notifications"
	notificationsPath = "/org/freedesktop/Notifications"
	notifyMethod      = notificationsName + ".Notify"

	// appName is reported to the daemon as the sending application.
	appName = "qubesome"

	// expireTimeout is how long the daemon shows a notification for, in
	// milliseconds.
	expireTimeout = 10000
)

// Notify posts a desktop notification on the session bus.
//
// It speaks to the bus directly rather than through dbus-send, which
// cannot express the hints argument at all. dbus-send's container types
// are array, dict and variant, but variant is not among the types a dict
// value may have, so the closest it can render an empty a{sv} as is
// a{ss}. Every notification qubesome sent was rejected for it, with
// "Type of message, (susssasa{ss}i), does not match expected type
// (susssasa{sv}i)".
func Notify(title, body string) error {
	// A private connection rather than the shared one, because a single
	// notification has no use for a connection that outlives it.
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return fmt.Errorf("cannot connect to the session bus: %w", err)
	}
	// The reply has already been read by the time this runs, so there
	// is nothing a close error could change.
	defer func() { _ = conn.Close() }()

	slog.Debug(notifyMethod, "title", title, "body", body)

	// Notify's arguments, in order: the sending application, the
	// notification to replace, an icon, the summary, the body, the
	// actions offered, the hints and how long to show it for. The
	// profile has no actions and no hints, but the signature has no room
	// to leave either out.
	call := conn.Object(notificationsName, notificationsPath).Call(notifyMethod, 0,
		appName,
		uint32(0),
		"",
		title,
		body,
		[]string{},
		map[string]dbus.Variant{},
		int32(expireTimeout),
	)
	if call.Err != nil {
		return fmt.Errorf("cannot send notification: %w", call.Err)
	}

	return nil
}

func NotifyOrLog(title, body string) {
	if strings.EqualFold(os.Getenv("XDG_SESSION_TYPE"), "wayland") {
		slog.Error("logging notification (sending not supported on wayland)", "title", title, "body", body)
	}

	err := Notify(title, body)
	if err != nil {
		slog.Error("cannot send notification", "error", err, "notification", body)
	}
}
