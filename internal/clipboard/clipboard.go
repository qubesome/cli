package clipboard

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/types"
	"golang.org/x/sys/execabs"
)

var ErrUnsupportedCopyType = errors.New("unsupported copy type")
var ErrCannotCopyClipboardWithinSameDisplay = errors.New("cannot copy clipboard within the same display")

func Copy(from uint8, to *types.Profile, target string) error {
	if !validTarget(target) {
		return fmt.Errorf("%w: %s", ErrUnsupportedCopyType, target)
	}

	if from == to.Display {
		return ErrCannotCopyClipboardWithinSameDisplay
	}

	targetExtra := ""
	if target != "" {
		targetExtra = fmt.Sprintf("-t %s", target)
	}

	magicCookie, err := files.ServerCookiePath(to.Name)
	if err != nil {
		return fmt.Errorf("cannot get X magic cookie path: %w", err)
	}

	xclip := fmt.Sprintf("/usr/bin/xclip -selection clip -o -display :%d | XAUTHORITY=%s /usr/bin/xclip -selection clip %s -i -display :%d",
		from, magicCookie, targetExtra, to.Display)

	slog.Debug("clipboard copy", "command", []string{"/bin/sh", "-c", xclip})
	cmd := execabs.Command("/bin/sh", "-c", xclip)

	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to copy clipboard: %w", err)
	}

	return nil
}

func validTarget(target string) bool {
	return (target == "" || target == "image/png")
}
