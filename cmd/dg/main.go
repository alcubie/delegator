// Command dg gives the commands of delegator to the person. It takes the
// directories and runs the command tree, and internal/cli holds each command.
package main

import (
	"fmt"
	"os"

	"github.com/alcubie/delegator/internal/cli"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "delegator:", err)
		os.Exit(1)
	}
}

func run() error {
	dataDir, err := cli.DataDir()
	if err != nil {
		return err
	}
	workDir, err := os.Getwd()
	if err != nil {
		return err
	}
	return cli.Root(dataDir, workDir).Execute()
}
