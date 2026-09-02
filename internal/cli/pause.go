package cli

import (
	"fmt"

	"github.com/alcubie/delegator/internal/store"
	"github.com/spf13/cobra"
)

const pausedMessage = "The queue is paused. Current runs will finish."

func pauseCommand(dataDir string) *cobra.Command {
	return &cobra.Command{
		Use:   "pause",
		Short: "Pause the queue.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return store.With(dataDir, func(s *store.Store) error {
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
