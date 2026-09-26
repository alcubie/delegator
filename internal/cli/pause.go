package cli

import (
	"fmt"
	"io"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/store"
	"github.com/spf13/cobra"
)

// pausedMessage points to dg cancel because pausing leaves active runs
// running.
const pausedMessage = "The queue is paused. Current runs will finish. Use dg cancel <id> to stop a ticket."

// pauseCommand returns the command for dg pause.
func pauseCommand(dataDir *string, cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "pause",
		Short: "Pause the queue.",
		Long: "Pause the queue so no new agent runs start. Runs already in progress are " +
			"allowed to finish.",
		Example: `  dg pause`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withStore(*dataDir, cfg, func(s *store.Store) error {
				err := s.PauseQueue()
				if err != nil {
					return err
				}

				return writeValue(cmd.OutOrStdout(), nil, false, func(out io.Writer) {
					fmt.Fprintln(out, pausedMessage)
				})
			})
		},
	}
}
