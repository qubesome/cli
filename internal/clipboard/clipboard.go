package clipboard

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"

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
	var profile string

	if o.SourceProfile != nil {
		from = o.SourceProfile.Display
		profile = o.SourceProfile.Name
	}

	if o.TargetProfile == nil && !o.ToHost {
		return fmt.Errorf("target profile cannot be nil when ToHost is false")
	}

	if o.TargetProfile != nil {
		target = o.TargetProfile.Display
		profile = o.TargetProfile.Name
	}

	if from == target {
		return ErrCannotCopyClipboardWithinSameDisplay
	}

	if !validTarget(o.ContentType) {
		return fmt.Errorf("%w: %s", ErrUnsupportedCopyType, o.ContentType)
	}

	cookiePath, err := files.ServerCookiePath(profile)
	if err != nil {
		return fmt.Errorf("cannot get X magic cookie path: %w", err)
	}

	out, in := copyCommands(from, target, o.ContentType, cookiePath)

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
func copyCommands(from, target uint8, contentType, cookiePath string) (out, in *execabs.Cmd) {
	out = execabs.Command(files.XclipBinary, //nolint:gosec
		"-selection", "clip",
		"-o",
		"-display", display(from),
	)

	inArgs := []string{"-selection", "clip"}
	if contentType != "" {
		inArgs = append(inArgs, "-t", contentType)
	}
	inArgs = append(inArgs, "-i", "-display", display(target))

	in = execabs.Command(files.XclipBinary, inArgs...) //nolint:gosec
	in.Env = append(os.Environ(), "XAUTHORITY="+cookiePath)

	return out, in
}

func display(d uint8) string {
	return ":" + strconv.Itoa(int(d))
}

// pipe connects the output of out to the input of in and runs both.
func pipe(out, in *execabs.Cmd) error {
	r, w := io.Pipe()
	out.Stdout = w
	in.Stdin = r

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
	return errors.Join(inErr, outErr)
}

func validTarget(target string) bool {
	return (target == "" || target == "image/png")
}
