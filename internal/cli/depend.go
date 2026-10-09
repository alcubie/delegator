// dg depend changes dependencies between existing tickets. Store validates
// and persists the links; this file parses command arguments.

package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/run"
	"github.com/alcubie/delegator/internal/store"
)

// dependCommand returns a fresh dependency command whose examples use
// commandPath. It updates dependencies silently on success; the inbox and dg
// show display them.
func dependCommand(dataDir *string, cfg *config.Config, commandPath string) *cobra.Command {
	var after []int64
	var remove bool
	cmd := &cobra.Command{
		Use:   "depend <id>",
		Short: "Add or remove dependencies of a queued ticket.",
		Long: "Make a queued ticket wait for one or more other tickets to be done. Use " +
			"--remove to remove the named dependency links instead.",
		Example: fmt.Sprintf(`  %s 42 --after 17
  %s 42 --after 17 --after 23
  %s 42 --after 17 --remove`, commandPath, commandPath, commandPath),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := ticketArg(args[0])
			if err != nil {
				return err
			}
			return withStore(*dataDir, cfg, func(s *store.Store) error {
				if remove {
					if err := s.RemoveDependencies(id, after...); err != nil {
						return err
					}
					return run.Next(s, *cfg, launchFrom(*dataDir, launch))
				}
				return s.AddDependencies(id, after...)
			})
		},
	}
	cmd.Flags().Int64SliceVar(&after, "after", nil,
		"the ticket ID to add or remove as a dependency (required; may be repeated or comma-separated)")
	cmd.Flags().BoolVar(&remove, "remove", false,
		"remove the named dependency links instead of adding them")
	// The flag was registered above, so MarkFlagRequired cannot fail.
	_ = cmd.MarkFlagRequired("after")
	return rpcOperationCommand("depend", cmd)
}
