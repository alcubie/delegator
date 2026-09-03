package cli

import (
	"fmt"

	"github.com/alcubie/delegator/internal/run"
	"github.com/alcubie/delegator/internal/store"
	"github.com/spf13/cobra"
)

const startMessage = "The queue is running."

// startCommand returns the command for dg start.
func startCommand(dataDir string) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start a paused queue.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return store.With(dataDir, func(s *store.Store) error {
				err := s.ResumeQueue()
				if err != nil {
					return err
				}

				fmt.Fprintln(cmd.OutOrStdout(), startMessage)
				return run.Next(s, launch)
			})
		},
	}
}
