package container

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"

	"github.com/qubesome/cli/internal/types"
	"golang.org/x/sys/execabs"
)

func ID(bin, name string) (string, bool) {
	args := fmt.Sprintf("ps -a -q -f name=%s", name)
	cmd := execabs.Command(bin, //nolint:gosec
		strings.Split(args, " ")...)

	out, err := cmd.Output()
	id := string(bytes.TrimSuffix(out, []byte("\n")))

	if err != nil || id == "" {
		return "", false
	}

	return id, true
}

func Exec(bin, id string, ew types.EffectiveWorkload) error {
	//nolint:prealloc
	args := []string{"exec", "--detach", id, ew.Workload.Command}
	args = append(args, ew.Workload.Args...)

	slog.Debug(bin+" exec", "container-id", id, "cmd", ew.Workload.Command, "args", ew.Workload.Args)
	cmd := execabs.Command(bin, args...)

	return cmd.Run()
}

func Running(bin, name string) bool {
	_, running := ID(bin, name)

	return running
}
