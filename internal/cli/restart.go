package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/run"
	"github.com/alcubie/delegator/internal/store"
)

// restartCommand resumes an explicitly named failed ticket directly,
// bypassing the queue. It preserves the branch, session, and worktree, and
// writes nothing on success.
func restartCommand(dataDir *string, cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "restart <id>",
		Short: "Start a failed ticket again.",
		Long: "Return a failed ticket to execution, reusing its branch, worktree, and agent " +
			"session so work can continue where the failed run stopped.",
		Example: `  dg restart 42`,
		Args:    cobra.ExactArgs(1),
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
				if ticket.Status != store.Failed {
					return fmt.Errorf("%w: the ticket is %s, and only a failed ticket restarts",
						store.ErrInvalidTicketStateChange, ticket.Status)
				}
				return run.Detach(launchIn(*dataDir, restartLaunch(id)))
			})
		},
	}
}
