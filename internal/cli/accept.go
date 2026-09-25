package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/run"
	"github.com/alcubie/delegator/internal/store"
)

// acceptCommand accepts an explicit ticket or the project's first ready
// ticket. An implicit selection prints the accepted ID.
//
// The merge check and worktree removal run inside the status transaction, so
// failures leave the ticket ready. --force skips the merge check and permits
// removing uncommitted changes.
func acceptCommand(dataDir *string, workDir string, cfg *config.Config) *cobra.Command {
	var projectDir string
	var force bool
	cmd := &cobra.Command{
		Use:   "accept [id]",
		Short: "Close a ready ticket.",
		Long: "Accept a ready ticket after its branch has been merged, mark it done, remove its " +
			"worktree, and start queued work if capacity is available. With no ID, accept the first ready ticket for the selected project.",
		Example: `  dg accept 42
  dg accept
  dg accept 42 --force`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withStore(*dataDir, cfg, func(s *store.Store) error {
				id, err := resolveTicketID(s, cfg, args, workDir, projectDir)
				if err != nil {
					return err
				}

				ticket, err := s.Ticket(id)
				if err != nil {
					return err
				}
				err = s.ChangeStatusWith(id, store.Done, func() error {
					if !force {
						if err := project.RequireBranchMerged(ticket.Project.Path, ticket.Branch); err != nil {
							return err
						}
					}
					return run.RemoveWorktree(*dataDir, ticket, force)
				})
				if err != nil {
					return err
				}
				if err := writeValue(cmd.OutOrStdout(), ticketID{ID: id}, false, func(out io.Writer) {
					if len(args) == 0 {
						fmt.Fprintln(out, id)
					}
				}); err != nil {
					return err
				}
				return run.Next(s, *cfg, launchFrom(*dataDir, launch))
			})
		},
	}
	cmd.Flags().StringVar(&projectDir, "project", "",
		"select the project whose first ready ticket to accept when ID is omitted (default: current working directory)")
	cmd.Flags().BoolVar(&force, "force", false,
		"skip the merge and clean-worktree checks; removing a dirty worktree loses its uncommitted changes")
	return cmd
}
