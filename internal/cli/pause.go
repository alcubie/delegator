package cli

import (
	"fmt"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/store"
	"github.com/spf13/cobra"
)

// pausedMessage is what dg pause writes. It names dg cancel because a pause
// leaves the run that is going, and a person who wanted that run to stop has
// to be told what stops it.
const pausedMessage = "The queue is paused. Current runs will finish. Use dg cancel <id> to stop one."

// pauseCommand returns the command for dg pause.
func pauseCommand(dataDir string, cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "pause",
		Short: "Pause the queue.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withStore(dataDir, cfg, func(s *store.Store) error {
				err := s.PauseQueue()
				if err != nil {
					return err
				}

				fmt.Fprintln(cmd.OutOrStdout(), pausedMessage)
				return nil
			})
		},
	}
}
