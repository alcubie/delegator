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

// acceptAvailable reports whether the status of a ticket lets accept run,
// and the reason beside it for when it does not. The reason is the message
// accept itself would print, so the two cannot drift apart.
//
// The merge and the worktree are accept's own validation, left to the run of
// accept. The status of a ticket changes only through dg, so the answer holds
// until dg changes it, while a branch merges and a worktree turns dirty
// outside dg and a reported answer would already be stale.
func acceptAvailable(ticket store.Ticket) (bool, string) {
	if !store.CanChange(ticket.Status, store.Done) {
		return false, fmt.Sprintf("%s: %s to %s",
			store.ErrInvalidTicketStateChange, ticket.Status, store.Done)
	}
	return true, ""
}

// acceptCommand accepts an explicit ticket or the project's first ready
// ticket. An implicit selection prints the accepted ID.
//
// Merge and worktree validation run inside the status transaction, so failures
// leave the ticket ready. Cleanup follows the durable transition and cannot
// reverse acceptance. --force skips merge and cleanliness validation.
func acceptCommand(dataDir *string, workDir string, cfg *config.Config, commandPath string) *cobra.Command {
	var projectDir string
	var force bool
	cmd := &cobra.Command{
		Use:   "accept [id]",
		Short: "Close a ready ticket.",
		Long: "Accept a ready ticket after its branch has been merged, mark it done, attempt to remove its " +
			"worktree, and start queued work if capacity is available. With no ID, accept the first ready ticket for the selected project.",
		Example: fmt.Sprintf(`  %s 42
  %s
  %s 42 --force`, commandPath, commandPath, commandPath),
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
				path := run.WorktreePath(*dataDir, id)
				var worktree run.AcceptanceWorktree
				err = s.ChangeStatusWith(id, store.Done, func() error {
					if !force {
						if err := project.RequireBranchMerged(ticket.Project.Path, ticket.Branch, ticket.Project.DefaultBranch); err != nil {
							return err
						}
					}
					var err error
					worktree, err = run.ValidateAcceptanceWorktree(path, force)
					return err
				})
				if err != nil {
					return err
				}
				switch worktree {
				case run.WorktreeRegistered:
					if err := run.RemoveWorktree(*dataDir, ticket, force); err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not remove worktree for ticket %d at %s: %v\n", id, path, err)
					}
				case run.WorktreeUnregistered:
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: ticket %d worktree at %s has no .git entry; preserving its contents because cleanliness could not be verified; inspect it manually\n", id, path)
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
	return rpcOperationCommand("accept", cmd)
}
