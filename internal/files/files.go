// Package files centralises location paths used in Qubesome.
//
// Key locations:
// - ~/.qubesome: default location for persistent files.
// - ~/.qubesome/images-last-checked: file that stores when images were last checked.
// - /run/user/%d/qubesome: root of ephemeral files.
// - /run/user/%d/qubesome/git/<git-url>/<path>: where git repositories
// are cloned to.
package files

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	securejoin "github.com/cyphar/filepath-securejoin"
)

const (
	FileMode = 0o600
)

var (
	// ErrUnableGetSocketPath is an error returned when unable to get the socket path for a profile.
	ErrUnableGetSocketPath = errors.New("unable to get socket path for profile")
)

// QubesomeDir returns the root directory where Qubesome configuration is stored.
func QubesomeDir() string {
	return os.ExpandEnv("${HOME}/.qubesome")
}

// QubesomeConfig returns the default qubesome config file path.
func QubesomeConfig() string {
	return filepath.Join(QubesomeDir(), "qubesome.config")
}

// ImagesLastCheckedPath returns the file path for the file that records
// when images where last checked.
func ImagesLastCheckedPath() string {
	return filepath.Join(QubesomeDir(), "images-last-checked")
}

// RunUserQubesome returns the path to the user-specific qubesome directory.
func RunUserQubesome() string {
	return fmt.Sprintf("/run/user/%d/qubesome", os.Getuid())
}

// ClientCookiePath returns the path to the client cookie file for the given profile.
func ClientCookiePath(profile string) (string, error) {
	base := RunUserQubesome()
	return securejoin.SecureJoin(base, fmt.Sprintf("%s/.Xclient-cookie", profile))
}

// ServerCookiePath returns the path to the server cookie file for the given profile.
func ServerCookiePath(profile string) (string, error) {
	base := RunUserQubesome()
	return securejoin.SecureJoin(base, fmt.Sprintf("%s/.Xserver-cookie", profile))
}

// SocketPath returns the path to the socket file for the given profile.
func SocketPath(profile string) (string, error) {
	base := RunUserQubesome()
	return securejoin.SecureJoin(base, fmt.Sprintf("%s/qube.sock", profile))
}

// GitRoot returns the root directory for git repositories.
func GitRoot() string {
	return filepath.Join(RunUserQubesome(), "git")
}

// GitDirPath returns the path to the git directory for the given URL.
func GitDirPath(url string) (string, error) {
	base := GitRoot()

	url = strings.ReplaceAll(url, ":", "/")
	url = strings.ReplaceAll(url, "git@", "")

	p, err := securejoin.SecureJoin(base, url)
	if err != nil {
		return "", fmt.Errorf("cannot get git dir path for %q: %w", url, err)
	}

	return p, nil
}

// WorkloadsDir returns the workloads directory path for a given Qubesome profile.
func WorkloadsDir(profile string) (string, error) {
	return securejoin.SecureJoin(QubesomeDir(), filepath.Join(profile, "workloads"))
}

// WorkloadFiles returns a list of workload file paths.
func WorkloadFiles() ([]string, error) {
	var matches []string
	root := GitRoot()
	pattern := fmt.Sprintf("^%s/.*/workloads/.*.yaml$", root)

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		matched, err := regexp.MatchString(pattern, path)
		if err != nil {
			return err
		}
		if matched {
			matches = append(matches, path)
		}
		return nil
	})
	return matches, err
}
