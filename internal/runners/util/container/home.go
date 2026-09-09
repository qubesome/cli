package container

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/qubesome/cli/internal/images"
)

// HomeDir returns the home directory of the user an unpacked image bundle
// runs as.
//
// It is read from the bundle's runtime spec, which umoci already resolved
// from the image configuration during unpack. Running the image to ask it
// where its home is would mean executing the workload's code, with the
// runner's default settings, to produce a value that is then used to build
// a bind mount target.
func HomeDir(b images.Bundle) (string, error) {
	return homeDir(b)
}

func homeDir(b images.Bundle) (string, error) {
	home := envValue(b.Env, "HOME")
	if home == "" {
		var err error
		if home, err = homeOfUser(b); err != nil {
			return "", err
		}
	}

	if !filepath.IsAbs(home) || home != filepath.Clean(home) {
		return "", fmt.Errorf("bundle %q has an invalid home dir: %q", b.Rootfs, home)
	}

	// The home dir is used to build a src:dst bind mount spec, where a
	// colon separates the fields.
	if strings.Contains(home, ":") {
		return "", fmt.Errorf("bundle %q has a home dir containing a colon: %q", b.Rootfs, home)
	}

	return home, nil
}

// homeOfUser derives a home directory from the uid a bundle's process runs
// as, using the convention that root lives in /root.
//
// Root is the only uid this can resolve. Every other uid needs a user name
// to build a /home/<name> path, and umoci already resolved that name
// against the image's own /etc/passwd while unpacking, without carrying it
// into the bundle, so there is no name left to build one from here.
func homeOfUser(b images.Bundle) (string, error) {
	if b.UID == 0 {
		return "/root", nil
	}

	return "", fmt.Errorf("bundle %q runs as uid %d: set HOME in the image to use mime handling", b.Rootfs, b.UID)
}

// envValue returns the value of key in an image's environment.
//
// The list may hold a key more than once. Runtimes build the container's
// environment by walking it in order, so the last entry is the one the
// process ends up with.
func envValue(env []string, key string) string {
	value := ""
	for _, e := range env {
		if k, v, ok := strings.Cut(e, "="); ok && k == key {
			value = v
		}
	}

	return value
}
