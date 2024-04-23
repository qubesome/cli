package files

import (
	"errors"
	"fmt"
	"os"
)

var (
	ErrUnableGetSocketPath = errors.New("unable to get socket path for profile")
)

func SocketPath(profile string) (string, error) {
	return fmt.Sprintf("/run/user/%d/qubesome/%s/qube.sock", os.Getuid(), profile), nil
}
