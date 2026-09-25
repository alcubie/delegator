// dg list shows tickets of every status, including those no longer in the
// inbox.

package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/store"
)

// listShort makes ticket listing discoverable; dg ticket list would create a
// ticket named list.
const listShort = "List every ticket, whatever its status"

// listCommand writes every ticket. --project narrows the list to one project.
func listCommand(dataDir *string, workDir string, cfg *config.Config) *cobra.Command {
	var projectDir string
	cmd := &cobra.Command{
		Use:   "list",
		Short: listShort,
		Long: "List tickets in every status and every project. Use --project to limit the " +
			"list to the repository at a particular directory.",
		Example: `  dg list
  dg list --project ../api`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			tickets, err := listTickets(*dataDir, workDir, cfg, projectDir)
			if err != nil {
				return err
			}
			return writeValue(cmd.OutOrStdout(), tickets, false, func(out io.Writer) {
				writeList(out, tickets)
			})
		},
	}
	cmd.Flags().StringVar(&projectDir, "project", "",
		"list only the project in this directory (default: every project)")
	return cmd
}

// listTickets selects all projects unless projectDir specifies --project.
func listTickets(dataDir, workDir string, cfg *config.Config, projectDir string) ([]store.OpenTicket, error) {
	var root string
	if projectDir != "" {
		dir, err := ticketProject(workDir, projectDir)
		if err != nil {
			return nil, err
		}
		if root, err = project.Root(dir); err != nil {
			return nil, err
		}
	}
	var tickets []store.OpenTicket
	err := withStore(dataDir, cfg, func(s *store.Store) error {
		var err error
		tickets, err = s.AllTickets(root)
		if err != nil {
			return err
		}
		return nil
	})
	return tickets, err
}

// writeList renders tickets in the supplied order, sharing inbox columns and
// showing status at the right. An empty list writes nothing.
func writeList(out io.Writer, tickets []store.OpenTicket) {
	idWidth, projectWidth := ticketWidths(tickets)
	rowWidth := outputWidth(out)
	for _, t := range tickets {
		fmt.Fprintln(out, ticketRow(t, idWidth, projectWidth, rowWidth, string(t.Status)))
	}
}
