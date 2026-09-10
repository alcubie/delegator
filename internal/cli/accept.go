package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/run"
	"github.com/alcubie/delegator/internal/store"
)

// acceptCommand returns the command dg accept. With no id it closes the head of
// READY, which is the ticket the person has just reviewed.
//
// The worktree is removed inside the transaction that closes the ticket, so a
// worktree git refuses leaves the ticket ready and a person sees it again.
func acceptCommand(dataDir, workDir string, cfg *config.Config) *cobra.Command {
	var projectDir string
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
					return run.RemoveWorktree(dataDir, ticket)
				})
				if err != nil {
					return err
				}
				// The person who typed an id knows which ticket went, and the
				// one who did not gets the id of the ticket this chose.
				if len(args) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), id)
				}
				return run.Next(s, launch)
			})
		},
	}
	cmd.Flags().StringVar(&projectDir, "project", "",
		"the directory of the project whose first ready ticket to close.  Defaults to current working directory.")
	return cmd
}
