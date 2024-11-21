package cli

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/urfave/cli/v3"
)

//go:embed autocomplete/zsh_autocomplete
var autocompleteZSH string

func completionCommand() *cli.Command {
	cmd := &cli.Command{
		Name:      "autocomplete",
		Usage:     "Generate autocomplete",
		UsageText: "source <(qubesome autocomplete)",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			fmt.Println(autocompleteZSH)
			return nil
		},
	}
	return cmd
}
