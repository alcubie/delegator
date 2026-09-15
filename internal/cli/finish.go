package cli

import (
	"github.com/alcubie/delegator/internal/config"
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

				return s.FinishTicket(id, args[1])
			})
		},
	}
}
