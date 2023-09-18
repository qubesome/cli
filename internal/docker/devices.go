package docker

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var HidrawPrefix = "HID_NAME="

func namedDevices(names []string) ([]string, error) {
	devs := []string{}

	hidraws, err := filepath.Glob("/sys/class/hidraw/*/device/uevent")
	if err != nil {
		return nil, fmt.Errorf("failed to get hidraw files: %w", err)
	}

	for _, fn := range hidraws {
		d, err := readFile(fn, names)
		if err != nil {
			return nil, fmt.Errorf("failed to get hidraw files: %w", err)
		}
		devs = append(devs, d...)
	}

	return devs, nil
}

func readFile(fn string, names []string) ([]string, error) {
	devs := []string{}

	f, err := os.Open(fn)
	if err != nil {
		return nil, fmt.Errorf("failed to open hidraw file: %w", err)
	}

	r := bufio.NewScanner(f)
	for r.Scan() {
		val := r.Text()

		if !strings.HasPrefix(val, HidrawPrefix) {
			continue
		}
		devName := strings.TrimPrefix(val, HidrawPrefix)

		for _, n := range names {
			if strings.HasPrefix(devName, n) {
				fdn := "/dev/" + strings.TrimPrefix(fn, "/sys/class/hidraw/")
				fdn = strings.TrimSuffix(fdn, "/device/uevent")
				devs = append(devs, fdn)
			}
		}
	}

	return devs, nil
}
