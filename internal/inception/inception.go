package inception

import (
	"log/slog"
	"os"

	"github.com/qubesome/cli/internal/files"
)

func Inside() bool {
	path := files.InProfileSocketPath()
	_, err := os.Stat(path)
	inside := (err == nil)

	slog.Debug("inception check", "inside", inside)
	return inside
}
