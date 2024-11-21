package inception

import (
	"os"

	"github.com/qubesome/cli/internal/files"
)

func Inside() bool {
	path := files.InProfileSocketPath()
	_, err := os.Stat(path)
	return (err == nil)
}
