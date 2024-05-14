//go:build !x11

package resolution

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"

	"github.com/qubesome/cli/internal/files"
	"golang.org/x/sys/execabs"
)

// Primary returns the screen resolution for the primary display.
func Primary() (string, error) {
	cmd := execabs.Command(files.XrandrBinary) //nolint
	output, err := cmd.Output()
	if err != nil {
		return "", err
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

	return "", fmt.Errorf("cannot get resolution")
}
