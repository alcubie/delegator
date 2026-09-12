// The command that links two tickets that are already there: dg depend makes
// one ticket depend on another, and --remove takes the link away. dg ticket
// says the same thing about a ticket as it is made. internal/store holds the
// link and the rules that come out of it, and this file takes the ids.

package cli

import (
	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/store"
)

// dependCommand returns the command dg depend. It writes nothing when it works,
// as dg move does: the inbox says what a queued ticket depends on, and a
// command that reads back what the person just typed adds a line to every
// script that uses it.
func dependCommand(dataDir string, cfg *config.Config) *cobra.Command {
	var after []int64
	var remove bool
	cmd := &cobra.Command{
		Use: "depend <id> --after <other>",
		Short: "Make a queued ticket depend on another. It does not start until " +
			"the other ticket is done.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := ticketArg(args[0])
			if err != nil {
				return err
			}
			return withStore(dataDir, cfg, func(s *store.Store) error {
				if remove {
					return s.RemoveDependencies(id, after...)
				}
				return s.AddDependencies(id, after...)
			})
		},
	}
	cmd.Flags().Int64SliceVar(&after, "after", nil,
		"the ticket ID this ticket depends on.  Can be repeated.")
	cmd.Flags().BoolVar(&remove, "remove", false,
		"remove the dependency link.")
	// The error of MarkFlagRequired is a flag that the command does not have,
	// and the line above gave it that one.
	_ = cmd.MarkFlagRequired("after")
	return cmd
}
