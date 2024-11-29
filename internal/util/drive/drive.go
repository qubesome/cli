package drive

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"
)

const mountsFile = "/proc/mounts"

var readFile = os.ReadFile

func Mounts(drive, mount string) (bool, error) {
	if drive == "" {
		return false, fmt.Errorf("drive is empty")
	}
	if mount == "" {
		return false, fmt.Errorf("mount is empty")
	}

	data, err := readFile(mountsFile)
	if err != nil {
		return false, fmt.Errorf("failed to open mounts file: %w", err)
	}

	r := bytes.NewReader(data)
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if fields[0] != drive {
			continue
		}

		if len(fields) <= 1 {
			continue
		}

		if fields[1] == mount {
			return true, nil
		}
	}

	return false, nil
}
