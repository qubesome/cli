package files

import (
	"errors"
	"fmt"
	"os"

	securejoin "github.com/cyphar/filepath-securejoin"
)

var (
	ErrUnableGetSocketPath = errors.New("unable to get socket path for profile")
)

func SocketPath(profile string) (string, error) {
	return fmt.Sprintf("/run/user/%d/qubesome/%s/qube.sock", os.Getuid(), profile), nil
}

func GitDirPath(url string) (string, error) {
	base := fmt.Sprintf("/run/user/%d/qubesome/git", os.Getuid())
	p, err := securejoin.SecureJoin(base, url)
	if err != nil {
		return "", fmt.Errorf("cannot get git dir path for %q: %w", url, err)
	}

	return p, nil
}

func ProfilePath() string {
	return os.ExpandEnv("${HOME}/.qubesome")
}
