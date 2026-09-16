package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/run"
	"github.com/alcubie/delegator/internal/store"
)

// launch returns the command that starts a supervisor: this program, dg, with
// "run" and no id. It is a variable so that a test can put a program it can
// observe in its place; the real one would start the test binary.
var launch = dgRun

// restartLaunch returns the supervisor for one failed ticket. It is separate
// from launch because a restarted ticket bypasses the queue.
var restartLaunch = dgRestart

// dgRun returns dg run for the executable that is running now. A dg started as
// ./dg from a build directory is not on the PATH, and the executable that is
// running is the one dg the person has.
//
// It names no ticket. The supervisor claims the ticket it works on, so a
// trigger that named one would be reading the queue a second time. dgRestart
// is the exception: it names the failed ticket that it must resume.
func dgRun() *exec.Cmd {
	return dgRunArgs()
}

func dgRestart(id int64) *exec.Cmd {
	return dgRunArgs("--restart", strconv.FormatInt(id, 10))
}

func dgRunArgs(args ...string) *exec.Cmd {
	exe, err := os.Executable()
	if err != nil {
		exe = "dg"
	}
	return exec.Command(exe, append([]string{"run"}, args...)...)
}

// runCommand returns the command dg run. It is hidden from dg help because
// delegator starts it and a person does not: a supervisor launches one for the
// next ticket, and this is the program it launches. Typing it still works,
// which is how a run is driven by hand.
func runCommand(dataDir string, cfg *config.Config) *cobra.Command {
	var restart bool
	cmd := &cobra.Command{
		Use:    "run [id]",
		Short:  "Run a ticket: make its worktree, start the agent, and wait.",
		Hidden: true,
		Args:   cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if restart && len(args) != 1 {
				return fmt.Errorf("dg run --restart needs a ticket id")
			}
			// With no id the supervisor reads the queue and claims in one
			// transaction, which is what a trigger starts; with one, a person
			// named the ticket.
			start := func(s *store.Store) (bool, error) { return run.StartNext(s, *cfg) }
			if len(args) == 1 {
				id, err := ticketArg(args[0])
				if err != nil {
					return err
				}
				start = func(s *store.Store) (bool, error) { return true, run.Start(s, id, *cfg) }
				if restart {
					start = func(s *store.Store) (bool, error) { return true, run.Restart(s, id, *cfg) }
				}
			}
			// A run that could not start does not start the next one. What
			// stopped it is the database or the repository of the project,
			// and the next run would meet the same fault. A supervisor that
			// claimed nothing does not start the next one either: the queue
			// is as the trigger that started this one found it, and a
			// supervisor that launched another for the same queue would make
			// a chain that does not end.
			return withStore(dataDir, cfg, func(s *store.Store) error {
				claimed, err := start(s)
				if err != nil {
					return err
				}
				if !claimed {
					return nil
				}
				return run.Next(s, *cfg, launch)
			})
		},
	}
	cmd.Flags().BoolVar(&restart, "restart", false, "resume a failed ticket")
	return cmd
}
