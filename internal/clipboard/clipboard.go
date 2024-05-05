package clipboard

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/types"
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

	var from uint8
	var to *types.Profile

	if o.SourceProfile != nil {
		from = o.SourceProfile.Display
	}

	if o.TargetProfile != nil {
		to = o.TargetProfile
	}

	if !validTarget(o.ContentType) {
		return fmt.Errorf("%w: %s", ErrUnsupportedCopyType, o.ContentType)
	}

	if from == to.Display {
		return ErrCannotCopyClipboardWithinSameDisplay
	}

	targetExtra := ""
	if o.ContentType != "" {
		targetExtra = fmt.Sprintf("-t %s", o.ContentType)
	}

	cookiePath, err := files.ServerCookiePath(to.Name)
	if err != nil {
		return fmt.Errorf("cannot get X magic cookie path: %w", err)
	}

	xclip := fmt.Sprintf("%s -selection clip -o -display :%d | XAUTHORITY=%s %s -selection clip %s -i -display :%d",
		files.XclipBinary, int(from), cookiePath, files.XclipBinary, targetExtra, int(to.Display))

	slog.Debug("clipboard copy", "command", []string{files.ShBinary, "-c", xclip})
	cmd := execabs.Command(files.ShBinary, "-c", xclip) //nolint

	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to copy clipboard: %w", err)
	}

	return nil
}

func validTarget(target string) bool {
	return (target == "" || target == "image/png")
}
