package files

import (
	"errors"
	"fmt"
	"os"
	"strings"

	securejoin "github.com/cyphar/filepath-securejoin"
)

var (
	ErrUnableGetSocketPath = errors.New("unable to get socket path for profile")
)

func ClientCookiePath(profile string) (string, error) {
	base := fmt.Sprintf("/run/user/%d/qubesome", os.Getuid())
	return securejoin.SecureJoin(base, fmt.Sprintf("%s/.Xclient-cookie", profile))
}

func ServerCookiePath(profile string) (string, error) {
	base := fmt.Sprintf("/run/user/%d/qubesome", os.Getuid())
	return securejoin.SecureJoin(base, fmt.Sprintf("%s/.Xserver-cookie", profile))
}

func SocketPath(profile string) (string, error) {
	return fmt.Sprintf("/run/user/%d/qubesome/%s/qube.sock", os.Getuid(), profile), nil
}

func GitDirPath(url string) (string, error) {
	base := fmt.Sprintf("/run/user/%d/qubesome/git", os.Getuid())

	url = strings.ReplaceAll(url, ":", "/")
	url = strings.ReplaceAll(url, "git@", "")

	p, err := securejoin.SecureJoin(base, url)
	if err != nil {
		return "", fmt.Errorf("cannot get git dir path for %q: %w", url, err)
	}

	return p, nil
}

func ProfilePath() string {
	return os.ExpandEnv("${HOME}/.qubesome")
}
