package cli

import (
	"fmt"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/store"
	"github.com/spf13/cobra"
)

// finishCommand returns the command dg finish
func finishCommand(dataDir *string, cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "finish <id> <commit>",
		Short: "Record the commit of a ticket and mark it Ready.",
		Long: "Record the ticket branch commit that contains the completed work and mark the " +
			"ticket ready for review. The commit must belong to the ticket's branch.",
		Example: `  dg finish 42 4f3c2b1`,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := ticketArg(args[0])
			if err != nil {
				return err
			}
			return withStore(*dataDir, cfg, func(s *store.Store) error {
				ticket, err := s.Ticket(id)
				if err != nil {
					return err
				}
				// Report the state before consulting Git, without passing an
				// unvalidated commit to a ticket that cannot be finished.
				if ticket.Status != store.Running && ticket.Status != store.Failed && ticket.Status != store.Ready {
					return fmt.Errorf("%w: %s to %s",
						store.ErrInvalidTicketStateChange, ticket.Status, store.Ready)
				}
				commit, err := project.CommitOnBranch(ticket.Project.Path, args[1], ticket.Branch)
				if err != nil {
					return err
				}
				return s.FinishTicket(id, commit)
			})
		},
	}
}
