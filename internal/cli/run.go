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

// launch builds a supervisor command without a ticket ID. Tests replace it to
// observe starts without launching the test binary as dg.
var launch = dgRun

// restartLaunch returns the supervisor for one failed ticket. It is separate
// from launch because a restarted ticket bypasses the queue.
var restartLaunch = dgRestart

// dgRun uses the current executable, which may not be on PATH. It leaves
// ticket selection to the supervisor's atomic claim; dgRestart instead names
// the failed ticket to resume.
func dgRun() *exec.Cmd {
	return dgRunArgs()
}

func dgRestart(id int64, model string) *exec.Cmd {
	args := []string{"--restart", strconv.FormatInt(id, 10)}
	if model != "" {
		args = append(args, "--model", model)
	}
	return dgRunArgs(args...)
}

func dgRunArgs(args ...string) *exec.Cmd {
	exe, err := os.Executable()
	if err != nil {
		exe = "dg"
	}
	return exec.Command(exe, append([]string{"run"}, args...)...)
}

// launchIn passes the selected instance to a detached supervisor. Flags after
// a subcommand are accepted by Cobra, and keeping the option in argv avoids an
// environment override that would silently affect unrelated invocations.
func launchIn(dataDir string, cmd *exec.Cmd) *exec.Cmd {
	cmd.Args = append(cmd.Args, "--data-dir", dataDir)
	return cmd
}

func launchFrom(dataDir string, next func() *exec.Cmd) func() *exec.Cmd {
	return func() *exec.Cmd { return launchIn(dataDir, next()) }
}

// runCommand provides the hidden supervisor entry point. Delegator launches
// it automatically, but it can also be invoked manually.
func runCommand(dataDir *string, cfg *config.Config) *cobra.Command {
	var restart bool
	var model string
	cmd := &cobra.Command{
		Use:    "run [id]",
		Short:  "Run a ticket: make its worktree, start the agent, and wait.",
		Hidden: true,
		Args:   cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			defer func() {
				if cfg.Telemetry != nil && *cfg.Telemetry {
					triggerTelemetry(*dataDir)
				}
			}()
			if cmd.Flags().Changed("model") && (!restart || model == "") {
				return fmt.Errorf("--model needs --restart and a model ID")
			}
			if restart && len(args) != 1 {
				return fmt.Errorf("dg run --restart needs a ticket id")
			}
			// Automatic starts claim the next eligible ticket; an
			// explicit ID selects one ticket.
			start := func(s *store.Store) (bool, error) { return run.StartNext(s, *cfg) }
			if len(args) == 1 {
				id, err := ticketArg(args[0])
				if err != nil {
					return err
				}
				start = func(s *store.Store) (bool, error) { return true, run.Start(s, id, *cfg) }
				if restart {
					start = func(s *store.Store) (bool, error) { return true, run.Restart(s, id, *cfg, model) }
				}
			}
			// Only a supervisor that claimed and successfully ran
			// a ticket triggers more work. Retrying after setup
			// failure can repeat the same fault; chaining
			// supervisors that claimed nothing can loop
			// indefinitely.
			return withStore(*dataDir, cfg, func(s *store.Store) error {
				claimed, err := start(s)
				if err != nil {
					return err
				}
				if !claimed {
					return nil
				}
				return run.Next(s, *cfg, launchFrom(*dataDir, launch))
			})
		},
	}
	cmd.Flags().BoolVar(&restart, "restart", false, "resume a failed ticket")
	cmd.Flags().StringVar(&model, "model", "", "model ID for the restarted run")
	return rpcOperationCommand("run", cmd)
}
