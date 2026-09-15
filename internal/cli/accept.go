package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/run"
	"github.com/alcubie/delegator/internal/store"
)

// acceptCommand returns the command dg accept. With no id it closes the head of
// READY, which is the ticket the person has just reviewed, and writes the id of
// the ticket it closed. A person who typed an id already knows which one went.
//
// The worktree is removed inside the transaction that closes the ticket, so a
// worktree git refuses leaves the ticket ready and a person sees it again.
// --force closes the ticket anyway and takes the changes with the worktree.
func acceptCommand(dataDir, workDir string, cfg *config.Config) *cobra.Command {
	var projectDir string
	var force bool
	cmd := &cobra.Command{
		Use:   "accept [id]",
		Short: "Close a ready ticket. Defaults to the first Ready ticket for the project.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withStore(dataDir, cfg, func(s *store.Store) error {
				id, err := resolveTicketID(s, cfg, args, workDir, projectDir)
				if err != nil {
					return err
				}

				ticket, err := s.Ticket(id)
				if err != nil {
					return err
				}
				err = s.ChangeStatusWith(id, store.Done, func() error {
					return run.RemoveWorktree(dataDir, ticket, force)
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
				return run.Next(s, *cfg, launch)
			})
		},
	}
	cmd.Flags().StringVar(&projectDir, "project", "",
		"the directory of the project whose first ready ticket to close.  Defaults to current working directory.")
	cmd.Flags().BoolVar(&force, "force", false,
		"remove the worktree even when it holds changes that are not committed.  The changes are lost.")
	return cmd
}
