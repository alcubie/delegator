// Command dg gives the commands of delegator to the person. It takes the
// directories and runs the command tree, and internal/cli holds each command.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/alcubie/delegator/internal/cli"
)

func main() {
	if err := run(); err != nil {
		// dg chat hands the terminal to the agent and waits, so the status of
		// dg is the status of that program. It wrote to the terminal itself,
		// so nothing is written here for it.
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
	// An empty data directory asks the command tree to choose the platform
	// default after Cobra has parsed a possible --data-dir override.
	return cli.Root("", workDir).Execute()
}
