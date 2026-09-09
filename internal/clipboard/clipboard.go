package clipboard

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/files"
	"golang.org/x/sys/execabs"
)

var (
	ErrUnsupportedCopyType                  = errors.New("unsupported copy type")
	ErrCannotCopyClipboardWithinSameDisplay = errors.New("cannot copy clipboard within the same display")
)

func Run(opts ...command.Option[Options]) error {
	o := &Options{}
	for _, opt := range opts {
		opt(o)
	}

	var from, target uint8
	var fromProfile, targetProfile string

	if o.SourceProfile != nil {
		from = o.SourceProfile.Display
		fromProfile = o.SourceProfile.Name
	}

	if o.TargetProfile == nil && !o.ToHost {
		return fmt.Errorf("target profile cannot be nil when ToHost is false")
	}

	if o.TargetProfile != nil {
		target = o.TargetProfile.Display
		targetProfile = o.TargetProfile.Name
	}

	if from == target {
		return ErrCannotCopyClipboardWithinSameDisplay
	}

	if !validTarget(o.ContentType) {
		return fmt.Errorf("%w: %s", ErrUnsupportedCopyType, o.ContentType)
	}

	// Each display authenticates separately, so each end needs the cookie
	// of the display it talks to. Giving only one end a cookie is what
	// made a copy between two profiles fail: the read authenticated
	// against whatever the host had, which is never a profile's cookie.
	//
	// The host end has no cookie of qubesome's. It keeps the environment
	// it was invoked with, which is the session's own.
	fromCookie, err := cookieFor(fromProfile)
	if err != nil {
		return err
	}

	targetCookie, err := cookieFor(targetProfile)
	if err != nil {
		return err
	}

	out, in := copyCommands(from, target, o.ContentType, fromCookie, targetCookie)

	slog.Debug("clipboard copy", "out", out.Args, "in", in.Args)

	if err := pipe(out, in); err != nil {
		return fmt.Errorf("failed to copy clipboard: %w", err)
	}

	return nil
}

// copyCommands returns the pair of xclip invocations that read the clipboard
// from one display and write it to another.
//
// Both are built as argv, so no value below is ever parsed as shell syntax.
func copyCommands(from, target uint8, contentType, fromCookie, targetCookie string) (out, in *execabs.Cmd) {
	out = execabs.Command(files.XclipBinary, //nolint:gosec
		"-selection", "clip",
		"-o",
		"-display", display(from),
	)
	out.Env = withCookie(fromCookie)

	inArgs := []string{"-selection", "clip"}
	if contentType != "" {
		inArgs = append(inArgs, "-t", contentType)
	}
	inArgs = append(inArgs, "-i", "-display", display(target))

	in = execabs.Command(files.XclipBinary, inArgs...) //nolint:gosec
	in.Env = withCookie(targetCookie)

	return out, in
}

// cookieFor returns the X cookie of a profile's display, and an empty
// path for the host, which authenticates with the session's own.
func cookieFor(profile string) (string, error) {
	if profile == "" {
		return "", nil
	}

	path, err := files.ServerCookiePath(profile)
	if err != nil {
		return "", fmt.Errorf("cannot get X magic cookie path: %w", err)
	}

	return path, nil
}

// withCookie returns the environment for one end of the copy.
//
// An empty cookie leaves the environment alone, which is what the host
// end wants. The environment may already carry an XAUTHORITY, and
// appending leaves two entries for the key. os/exec keeps the last value
// of a duplicated key, so the cookie set here is the one xclip reads.
func withCookie(cookiePath string) []string {
	if cookiePath == "" {
		return nil
	}

	return append(os.Environ(), "XAUTHORITY="+cookiePath)
}

func display(d uint8) string {
	return ":" + strconv.Itoa(int(d))
}

// pipe connects the output of out to the input of in and runs both.
func pipe(out, in *execabs.Cmd) error {
	r, w := io.Pipe()
	out.Stdout = w
	in.Stdin = r

	// xclip says why on stderr and says nothing through its exit status,
	// which is 1 for a display it cannot open, a display it cannot
	// authenticate to and a selection that is empty alike. Without this
	// the caller is told "exit status 1" and has no way to tell those
	// apart.
	var outLog, inLog bytes.Buffer
	out.Stderr = &outLog
	in.Stderr = &inLog

	defer func() {
		if s := strings.TrimSpace(outLog.String()); s != "" {
			slog.Error("clipboard read", "display", displayOf(out), "error", s)
		}
		if s := strings.TrimSpace(inLog.String()); s != "" {
			slog.Error("clipboard write", "display", displayOf(in), "error", s)
		}
	}()

	if err := in.Start(); err != nil {
		// Nothing is going to read or write these ends now. Left open,
		// anything already blocked on them would stay blocked.
		_ = w.Close()
		_ = r.Close()

		return fmt.Errorf("cannot start %s: %w", in.Path, err)
	}

	outErr := out.Run()
	// Closing the write end lets the reading command see EOF. The error
	// from the writer is carried over so it does not read a truncated
	// clipboard as a complete one.
	_ = w.CloseWithError(outErr)

	inErr := in.Wait()
	_ = r.Close()

	// Both are reported. When the reading command fails, the writer sees
	// a broken pipe, and returning only that would hide the failure that
	// caused it behind its own symptom.
	return errors.Join(withStderr(inErr, &inLog), withStderr(outErr, &outLog))
}

// withStderr attaches what a command said to why it failed.
func withStderr(err error, log *bytes.Buffer) error {
	if err == nil {
		return nil
	}

	if s := strings.TrimSpace(log.String()); s != "" {
		return fmt.Errorf("%w: %s", err, s)
	}

	return err
}

// displayOf returns the display a command was pointed at, for a message
// that says which of the two ends is being reported.
func displayOf(cmd *execabs.Cmd) string {
	for i, a := range cmd.Args {
		if a == "-display" && i+1 < len(cmd.Args) {
			return cmd.Args[i+1]
		}
	}

	return ""
}

func validTarget(target string) bool {
	return (target == "" || target == "image/png")
}
