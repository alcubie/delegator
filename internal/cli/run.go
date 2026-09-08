package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/adapters"
	"github.com/alcubie/delegator/internal/run"
	"github.com/alcubie/delegator/internal/store"
)

// agent is the adapter that dg run starts. It is a variable so that a test can
// put the fake agent in its place.
var agent adapters.Adapter = adapters.Claude{}

// launch returns the command that starts a run for one ticket: this program,
// dg, with "run <id>". It is a variable so that a test can put a program it can
// observe in its place; the real one would start the test binary.
var launch = dgRun

// dgRun returns dg run <id> for the executable that is running now. A dg
// started as ./dg from a build directory is not on the PATH, and the
// executable that is running is the one dg the person has.
func dgRun(id int64) *exec.Cmd {
	exe, err := os.Executable()
	if err != nil {
		exe = "dg"
	}
	return exec.Command(exe, "run", strconv.FormatInt(id, 10))
}

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
			// A run that could not start does not start the next one. What
			// stopped it is the database or the repository of the project,
			// and the next run would meet the same fault.
			return store.With(dataDir, func(s *store.Store) error {
				if err := run.Start(s, id, agent); err != nil {
					return err
				}
				return run.Next(s, launch)
			})
		},
	}
}
