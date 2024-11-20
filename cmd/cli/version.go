package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

var version string = "(dev1)"

func versionCommand() *cli.Command {
	cmd := &cli.Command{
		Name:    "version",
		Aliases: []string{"v"},
		Usage:   "shows version and build information",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			fmt.Println(shortVersion())
			return nil
		},
	}
	return cmd
}

func shortVersion() string {
	return fmt.Sprintf("github.com/qubesome/cli %s", version)
}
