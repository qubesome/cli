package main

import (
	"context"
	"fmt"
	"os"

	"github.com/qubesome/cli/cmd/cli"
)

func main() {
	cmd := cli.RootCommand()

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
