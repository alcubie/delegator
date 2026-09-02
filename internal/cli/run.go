package cli

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/adapters"
	"github.com/alcubie/delegator/internal/run"
)

// agent is the adapter that dg run starts. It is a variable so that a test can
// put the fake agent in its place.
var agent adapters.Adapter = adapters.Claude{}

// runCommand returns the command dg run. It is hidden from dg help because
// delegator starts it and a person does not: a supervisor launches one for the
// next ticket, and this is the program it launches. Typing it still works,
// which is how a run is driven by hand.
func runCommand(dataDir string) *cobra.Command {
	return &cobra.Command{
		Use:    "run <id>",
		Short:  "Run one ticket: make its worktree, start the agent, and wait.",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("%q is not the id of a ticket", args[0])
			}
			return run.Start(dataDir, id, agent)
		},
	}
}
