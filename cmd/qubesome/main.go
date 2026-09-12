package main

import (
	"context"
	"fmt"
	"os"

	"github.com/qubesome/cli/cmd/cli"
)

func main() {
	cmd := cli.RootCommand()

	// Not stdout. Some commands have a caller reading their standard
	// output as data rather than as text for a person: tunnel puts an ssh
	// connection through it, and a diagnostic written there would be read
	// as the far end talking.
	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
