// Command dg runs delegator's command tree, implemented in internal/cli.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/alcubie/delegator/internal/cli"
)

func main() {
	if err := run(); err != nil {
		// dg chat inherits the agent's exit status. The agent already
		// wrote its output to the terminal.
		var exit cli.ExitError
		if errors.As(err, &exit) {
			os.Exit(exit.Code)
		}
		fmt.Fprintln(os.Stderr, "delegator:", err)
		os.Exit(1)
	}
}

func run() error {
	workDir, err := os.Getwd()
	if err != nil {
		return err
	}
	return cli.Root(workDir).Execute()
}
