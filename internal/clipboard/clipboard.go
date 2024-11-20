package clipboard

import (
	"errors"
	"fmt"
	"log/slog"

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

	targetExtra := ""
	if o.ContentType != "" {
		targetExtra = fmt.Sprintf("-t %s", o.ContentType)
	}

	cookiePath, err := files.ServerCookiePath(profile)
	if err != nil {
		return fmt.Errorf("cannot get X magic cookie path: %w", err)
	}

	xclip := fmt.Sprintf("%s -selection clip -o -display :%d | XAUTHORITY=%s %s -selection clip %s -i -display :%d",
		files.XclipBinary, int(from), cookiePath, files.XclipBinary, targetExtra, int(target))

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
