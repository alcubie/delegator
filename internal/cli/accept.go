package cli

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/run"
	"github.com/alcubie/delegator/internal/store"
)

// acceptCommand returns the command dg accept.
//
// The worktree goes before the status, as it does in run.Start. Git refuses a
// worktree holding changes that are not committed, and a ticket that stays
// ready after that refusal is one a person sees again.
func acceptCommand(dataDir string) *cobra.Command {
	return &cobra.Command{
		Use:   "accept <id>",
		Short: "Close a ready ticket.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("%q is not the id of a ticket", args[0])
			}
			s, err := store.Open(dataDir)
			if err != nil {
				return err
			}
			defer s.Close()

			ticket, err := s.Ticket(id)
			if err != nil {
				return err
			}
			// Removing the worktree cannot be undone, so refuse a change the
			// state machine will not take rather than discover it afterwards.
			if !store.CanChange(ticket.Status, store.Done) {
				return fmt.Errorf("%w: %s to %s",
					store.ErrInvalidTicketStateChange, ticket.Status, store.Done)
			}
			if err := run.RemoveWorktree(dataDir, ticket); err != nil {
				return err
			}
			return s.ChangeStatus(id, store.Done)
		},
	}
}
