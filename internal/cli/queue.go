package cli

import (
	"github.com/alcubie/delegator/internal/config"
	"github.com/spf13/cobra"
)

// queueCommand is the namespace for controls that affect the whole queue.
func queueCommand(dataDir *string, cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "queue",
		Short:   "Control the queue.",
		Long:    "Pause or start work across the queue.",
		Example: "  dg queue pause\n  dg queue start",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(pauseCommand(dataDir, cfg, "dg queue pause"))
	cmd.AddCommand(startCommand(dataDir, cfg, "dg queue start"))
	return cmd
}
