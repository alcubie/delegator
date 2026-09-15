package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/run"
	"github.com/alcubie/delegator/internal/store"
)

// restartCommand returns the command dg restart. It takes the id of a ticket
// and no default: dg accept closes the head of READY because that is the
// ticket the person is reading, and a person who restarts has read a failure
// and says which ticket it was.
//
// The restarted supervisor takes the ticket directly from failed to running.
// It keeps its branch, its session and its worktree, so the run continues the
// work of the run that failed. The command writes nothing, because the person
// named the ticket.
func restartCommand(dataDir string, cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "restart <id>",
		Short: "Start a failed ticket again.",
		Args:  cobra.ExactArgs(1),
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
				if ticket.Status != store.Failed {
					return fmt.Errorf("%w: the ticket is %s, and only a failed ticket restarts",
						store.ErrInvalidTicketStateChange, ticket.Status)
				}
				return run.Detach(restartLaunch(id))
			})
		},
	}
}
