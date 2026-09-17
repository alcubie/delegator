// The command dg list: every ticket of every status, one to a line. The inbox
// is the work of a day, and this is the record behind it, for a person or an
// agent who wants a ticket the inbox no longer shows.

package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/store"
)

// listShort is the line the help gives for dg list. A person who wanted the
// whole list guessed at dg ticket list, which made a ticket named list, so the
// line has to say that this is the command that shows them all.
const listShort = "List every ticket, whatever its status"

// listCommand writes every ticket. --project narrows the list to one project.
func listCommand(dataDir *string, workDir string, cfg *config.Config) *cobra.Command {
	var projectDir string
	cmd := &cobra.Command{
		Use:   "list",
		Short: listShort,
		Args:  cobra.NoArgs,
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
		"the directory of the project.  Defaults to every project.")
	return cmd
}

// listTickets reads the tickets that dg list writes. projectDir is the value
// of --project, and the empty string is every project: a person who gives no
// directory asked for the whole list, and dg ticket has no such reading to
// take from it because a ticket belongs to one project.
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

// writeList writes one row for each ticket, in the order it is given, with the
// status of the ticket at the right of the row. The columns are the inbox's,
// so a person who reads one list reads the other.
//
// A list with no ticket writes nothing at all. The inbox says that there is no
// ticket because it is the page a person opens each day and an empty one has
// to say what it means; this list is asked a question and answers it, and a
// line of prose in it is a line that whatever reads the list has to know.
func writeList(out io.Writer, tickets []store.OpenTicket) {
	idWidth, projectWidth := ticketWidths(tickets)
	for _, t := range tickets {
		fmt.Fprintln(out, ticketRow(t, idWidth, projectWidth, string(t.Status)))
	}
}
