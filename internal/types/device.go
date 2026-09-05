package types

import (
	"fmt"
	"path/filepath"
	"strings"
)

// maxDeviceLen bounds a device request, matching the bound on the other
// path-like fields.
const maxDeviceLen = 500

// ParseDevice splits a device request into its source, destination and
// permissions components.
//
// The accepted format matches the one used by the container runners:
//
//	src[:dst[:perms]]
//
// Both src and dst must be paths under /dev, and perms may only contain
// the r, w and m flags. When omitted, dst defaults to src and perms
// defaults to rwm, mirroring the runner defaults.
func ParseDevice(device string) (src, dst, perms string, err error) {
	// Bound the input before splitting it or quoting it back. Device
	// requests come from a workload config, and an oversized one would
	// otherwise be allocated per field and echoed into an error and the
	// log line that reports it.
	if len(device) > maxDeviceLen {
		return "", "", "", fmt.Errorf("invalid device: longer than %d characters", maxDeviceLen)
	}

	parts := strings.Split(device, ":")
	if len(parts) > 3 {
		return "", "", "", fmt.Errorf("invalid device %q: too many fields", device)
	}

	src = parts[0]
	dst = src
	perms = "rwm"

	if len(parts) > 1 {
		dst = parts[1]
	}
	if len(parts) > 2 {
		perms = parts[2]
	}

	if err := validDevicePath(src); err != nil {
		return "", "", "", fmt.Errorf("invalid device %q: %w", device, err)
	}
	if err := validDevicePath(dst); err != nil {
		return "", "", "", fmt.Errorf("invalid device %q: %w", device, err)
	}
	if err := validDevicePerms(perms); err != nil {
		return "", "", "", fmt.Errorf("invalid device %q: %w", device, err)
	}

	return src, dst, perms, nil
}

// ValidateDeviceGrant checks an entry of a profile's device allowlist.
//
// A grant names a source device, so it is held to the same rules as the
// source of a workload's device request.
func ValidateDeviceGrant(device string) error {
	if err := valid(device, "devices", maxDeviceLen, false, nil); err != nil {
		return err
	}

	if err := validDevicePath(device); err != nil {
		return fmt.Errorf("invalid device grant: %w", err)
	}

	return nil
}

// validDevicePerms reports whether perms is a set of device permissions.
//
// Each of r, w and m may appear at most once. A repeated flag adds nothing
// and only ever means the value was not what its author intended.
func validDevicePerms(perms string) error {
	if !devicePermsRegex.MatchString(perms) {
		return fmt.Errorf("permissions %q must only contain r, w and m", perms)
	}

	var seen [3]bool
	for _, p := range perms {
		i := strings.IndexRune("rwm", p)
		if seen[i] {
			return fmt.Errorf("permissions %q repeat %q", perms, string(p))
		}
		seen[i] = true
	}

	return nil
}

// validDevicePath reports whether path is a clean path under /dev.
func validDevicePath(path string) error {
	if !devicePathRegex.MatchString(path) {
		return fmt.Errorf("%q is not a path under /dev", path)
	}
	if path != filepath.Clean(path) {
		return fmt.Errorf("%q is not a clean path", path)
	}

	return nil
}
