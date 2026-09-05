package container

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/execabs"
)

// imageConfig holds the subset of an image's configuration needed to work
// out where its home directory is.
//
//nolint:tagliatelle // field names are set by the OCI image config schema.
type imageConfig struct {
	User string   `json:"User"`
	Env  []string `json:"Env"`
}

// HomeDir returns the home directory of the user an image runs as.
//
// It is read from the image configuration. Running the image to ask it
// where its home is would mean executing the workload's code, with the
// runner's default settings, to produce a value that is then used to build
// a bind mount target.
func HomeDir(bin, image string) (string, error) {
	args := []string{"image", "inspect", "--format", "{{json .Config}}", image}

	slog.Debug(bin + " " + strings.Join(args, " "))
	cmd := execabs.Command(bin, args...)

	out, err := cmd.Output()
	if err != nil {
		// cmd.Output captures stderr on failure, and the runner puts
		// the reason there: image missing, registry auth, and so on.
		var ee *execabs.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("failed to inspect image %q: %w: %s",
				image, err, bytes.TrimSpace(ee.Stderr))
		}

		return "", fmt.Errorf("failed to inspect image %q: %w", image, err)
	}

	var cfg imageConfig
	if err := json.Unmarshal(out, &cfg); err != nil {
		return "", fmt.Errorf("failed to parse config of image %q: %w", image, err)
	}

	return homeDir(cfg, image)
}

func homeDir(cfg imageConfig, image string) (string, error) {
	home := envValue(cfg.Env, "HOME")
	if home == "" {
		var err error
		if home, err = homeOfUser(cfg.User, image); err != nil {
			return "", err
		}
	}

	if !filepath.IsAbs(home) || home != filepath.Clean(home) {
		return "", fmt.Errorf("image %q has an invalid home dir: %q", image, home)
	}

	// The home dir is used to build a src:dst bind mount spec, where a
	// colon separates the fields.
	if strings.Contains(home, ":") {
		return "", fmt.Errorf("image %q has a home dir containing a colon: %q", image, home)
	}

	return home, nil
}

// homeOfUser derives a home directory from the user an image runs as, using
// the convention every qubesome image follows: root lives in /root and
// everyone else in /home/<name>.
func homeOfUser(user, image string) (string, error) {
	// The user may carry a group, as in "chrome:chrome", and nothing else.
	parts := strings.Split(user, ":")
	if len(parts) > 2 {
		return "", fmt.Errorf("image %q runs as %q, which is not user[:group]", image, user)
	}

	name := parts[0]

	if name == "" || name == "root" || name == "0" {
		return "/root", nil
	}

	// A numeric id names no directory. The image is the only place that
	// mapping exists, and reading it would mean running the image.
	if _, err := strconv.Atoi(name); err == nil {
		return "", fmt.Errorf("image %q runs as uid %s: set HOME in the image to use mime handling", image, name)
	}

	return "/home/" + name, nil
}

func envValue(env []string, key string) string {
	for _, e := range env {
		if k, v, ok := strings.Cut(e, "="); ok && k == key {
			return v
		}
	}

	return ""
}
