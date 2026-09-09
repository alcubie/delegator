package cli

import (
	"fmt"
	"strconv"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/store"
	"github.com/spf13/cobra"
)

// finishCommand returns the command dg finish
func finishCommand(dataDir string, cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "finish <id> <commit>",
		Short: "Finish a Running ticket and mark as Ready.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("%q is not the id of a ticket", args[0])
			}
			return withStore(dataDir, cfg, func(s *store.Store) error {

				return s.FinishTicket(id, args[1])
			})
		},
	}
}
