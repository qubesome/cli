package log

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"log/syslog"
	"os"
	"path/filepath"
)

const (
	// The $XDG_STATE_HOME contains state data that should persist between (application) restarts, but that is not important or portable enough to the user that it should be stored in $XDG_DATA_HOME.
	// It may contain: actions history (logs, history, recently used files, …)
	//
	// https://specifications.freedesktop.org/basedir-spec/basedir-spec-latest.html
	xdgStateVar     = "XDG_STATE_HOME"
	xdgStateDefault = "${HOME}/.local/state"

	syslogTag   = "qubesome"
	logDir      = "qubesome"
	logFileName = "qubesome.log"
	logFileMode = 0o600
	logDirMode  = 0o700
)

var (
	ErrRelativePathLogFile = errors.New("log path cannot be relative: check HOME and XDG_STATE_HOME env vars")

	lookupEnv = os.LookupEnv
)

func Configure(level string, toStdout, toFile, toSyslog bool) error {
	writers := []io.Writer{}

	if toStdout {
		writers = append(writers, os.Stdout)
	}

	if toFile {
		path := logPath()
		if !filepath.IsAbs(path) {
			return ErrRelativePathLogFile
		}

		err := os.MkdirAll(filepath.Dir(path), logDirMode)
		if err != nil {
			return err
		}

		f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, logFileMode)
		if err != nil {
			return err
		}

		writers = append(writers, f)
	}

	if toSyslog {
		w, err := syslog.New(syslogPriority(level), syslogTag)
		if err != nil {
			return fmt.Errorf("failed to create syslog writer: %w", err)
		}

		writers = append(writers, w)
	}

	if len(writers) == 0 {
		return nil
	}

	mw := io.MultiWriter(writers...)
	slog.SetDefault(slog.New(
		slog.NewTextHandler(mw,
			&slog.HandlerOptions{Level: slogLevel(level)}),
	))

	return nil
}

func logPath() string {
	base := os.ExpandEnv(xdgStateDefault)
	if v, ok := lookupEnv(xdgStateVar); ok {
		base = v
	}

	return filepath.Join(base, logDir, logFileName)
}

func slogLevel(logLevel string) slog.Level {
	switch logLevel {
	case "ERROR":
		return slog.LevelError
	case "DEBUG":
		return slog.LevelDebug
	case "INFO":
		fallthrough
	default:
		return slog.LevelInfo
	}
}

func syslogPriority(logLevel string) syslog.Priority {
	switch logLevel {
	case "ERROR":
		return syslog.LOG_USER | syslog.LOG_ERR
	case "DEBUG":
		return syslog.LOG_USER | syslog.LOG_DEBUG
	case "INFO":
		fallthrough
	default:
		return syslog.LOG_USER | syslog.LOG_INFO
	}
}
