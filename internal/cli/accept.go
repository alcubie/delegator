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
// The worktree is removed inside the transaction that closes the ticket, so a
// worktree git refuses leaves the ticket ready and a person sees it again.
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
			return store.With(dataDir, func(s *store.Store) error {

				ticket, err := s.Ticket(id)
				if err != nil {
					return err
				}
				return s.ChangeStatusWith(id, store.Done, func() error {
					return run.RemoveWorktree(dataDir, ticket)
				})
			})
		},
	}
}
