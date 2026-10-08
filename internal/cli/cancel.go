package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/run"
	"github.com/alcubie/delegator/internal/store"
)

// cancelCommand requires an explicit ticket ID. Cancellation preserves the
// worktree so unfinished work remains available for inspection.
func cancelCommand(dataDir *string, cfg *config.Config, commandPath string) *cobra.Command {
	return rpcOperationCommand("cancel", &cobra.Command{
		Use:   "cancel <id>",
		Short: "Stop the work on a ticket and close it.",
		Long: "Stop a ticket's running agent when necessary and mark the ticket cancelled. " +
			"The ticket's worktree is kept so uncommitted work can still be inspected.",
		Example: fmt.Sprintf("  %s 42", commandPath),
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := ticketArg(args[0])
			if err != nil {
				return err
			}
			return withStore(*dataDir, cfg, func(s *store.Store) error {
				return cancelTicket(s, cfg, id)
			})
		},
	})
}

// cancelTicket stops a running supervisor before marking the ticket
// cancelled. Reconciliation has already marked dead or timed-out runs failed;
// do not signal those PIDs, which may now belong to unrelated processes.
//
// Signal outside the database transaction to avoid holding SQLite's writer
// lock through the grace period. Cancel records the end time if the stopped
// supervisor could not.
func cancelTicket(s *store.Store, cfg *config.Config, id int64) error {
	ticket, err := s.Ticket(id)
	if err != nil {
		return err
	}
	// Keep the stopped run ID for the database update; a restart may
	// create a newer run.
	var runID int64
	if ticket.Status == store.Running {
		r, err := s.Run(id)
		if err != nil {
			return err
		}
		if err := run.Stop(r.PID, run.StopGrace); err != nil {
			return err
		}
		runID = r.ID
	}
	if err := s.Cancel(id, runID); err != nil {
		return err
	}
	// Replace the stopped supervisor's scheduling trigger.
	return run.Next(s, *cfg, launchFrom(s.DataDir(), launch))
}
