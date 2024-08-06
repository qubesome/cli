package version

import (
	"fmt"
	"runtime/debug"

	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/qubesome"
)

var version string

type handler struct {
}

func New() command.Handler[qubesome.Options] {
	return &handler{}
}

func (c *handler) Handle(in command.App) (command.Action[qubesome.Options], []command.Option[qubesome.Options], error) {
	return c, nil, nil
}

func (c *handler) Run(opts ...command.Option[qubesome.Options]) error {
	if info, ok := debug.ReadBuildInfo(); ok {
		rev := "unknown"
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				rev = s.Value
				break
			}
		}
		fmt.Printf("%s %s\nRevision: %s\nGo: %s\n",
			info.Main.Path, version, rev, info.GoVersion)
	} else {
		fmt.Printf("github.com/qubesome/cli %s\n", version)
	}

	return nil
}
