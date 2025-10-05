//go:build !x11

package resolution

import (
	"bufio"
	"bytes"
	"fmt"
	"log/slog"
	"strings"

	"github.com/qubesome/cli/internal/files"
	"golang.org/x/sys/execabs"
)

const defaultResolution = "1440x1080"

// Primary returns the screen resolution for the primary display.
func Primary() (string, error) {
	binaries := []string{files.XrandrBinary, files.WlrRandrBinary}
	var output []byte
	var err error

	for _, binary := range binaries {
		cmd := execabs.Command(binary) //nolint
		output, err = cmd.Output()
		if err == nil && len(output) > 0 {
			break
		}

		slog.Debug("could not get resolution via %s: %w", binary, err)
	}

	if len(output) == 0 {
		slog.Debug("falling back to default resolution", "resolution", defaultResolution)
		return defaultResolution, nil
	}

	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		text := strings.TrimSpace(scanner.Text())
		if !strings.Contains(text, "*") {
			continue
		}

		fields := strings.Fields(text)
		if len(fields) == 0 {
			continue
		}

		raw := fields[0]
		if strings.Contains(raw, "x") {
			return raw, nil
		}
	}

	return "", fmt.Errorf("cannot get resolution from output: %q", output)
}
