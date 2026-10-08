package cli

import (
	"fmt"
	"io"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/run"
	"github.com/alcubie/delegator/internal/store"
	"github.com/spf13/cobra"
)

const startMessage = "The queue is running."

// startCommand returns the command for dg start.
func startCommand(dataDir *string, cfg *config.Config) *cobra.Command {
	return rpcOperationCommand("start", &cobra.Command{
		Use:   "start",
		Short: "Start a paused queue.",
		Long: "Resume a paused queue and start queued tickets until the configured run " +
			"limits are filled.",
		Example: `  dg start`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withStore(*dataDir, cfg, func(s *store.Store) error {
				err := s.ResumeQueue()
				if err != nil {
					return err
				}

				if err := writeValue(cmd.OutOrStdout(), nil, false, func(out io.Writer) {
					fmt.Fprintln(out, startMessage)
				}); err != nil {
					return err
				}
				return run.Next(s, *cfg, launchFrom(*dataDir, launch))
			})
		},
	})
}
