package cli

import (
	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/store"
	"github.com/spf13/cobra"
)

// finishCommand returns the command dg finish
func finishCommand(dataDir string, cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "finish <id> <commit>",
		Short: "Record the commit of a ticket and mark it Ready.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := ticketArg(args[0])
			if err != nil {
				return err
			}
			return withStore(dataDir, cfg, func(s *store.Store) error {
				ticket, err := s.Ticket(id)
				if err != nil {
					return err
				}
				// Let FinishTicket report an invalid state before consulting Git,
				// so a bad commit does not hide that the ticket cannot be finished.
				if ticket.Status != store.Running && ticket.Status != store.Ready {
					return s.FinishTicket(id, args[1])
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
