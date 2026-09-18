// The command dg search: find tickets by the words in their titles and prose.

package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/store"
)

// searchShort is the line the help gives for dg search.
const searchShort = "Find tickets by their text"

// searchCommand finds pattern in a title or in the Markdown prose. --project
// takes the same directory as it does on dg show and dg list.
func searchCommand(dataDir *string, workDir string, cfg *config.Config) *cobra.Command {
	var projectDir string
	cmd := &cobra.Command{
		Use:   "search <pattern>",
		Short: searchShort,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			found, err := searchTickets(*dataDir, workDir, cfg, projectDir, args[0])
			if err != nil {
				return err
			}
			writeSearch(cmd.OutOrStdout(), found)
			return nil
		},
	}
	cmd.Flags().StringVar(&projectDir, "project", "",
		"the directory of the project.  Defaults to every project.")
	return cmd
}

// searchTickets reads the prose of every ticket that the project asks for.
// The prose stays in its file, which is the thing a person edits, rather than
// becoming a second copy in the database.
func searchTickets(dataDir, workDir string, cfg *config.Config, projectDir, pattern string) ([]store.OpenTicket, error) {
	tickets, err := listTickets(dataDir, workDir, cfg, projectDir)
	if err != nil {
		return nil, err
	}

	pattern = strings.ToLower(pattern)
	found := make([]store.OpenTicket, 0)
	for _, ticket := range tickets {
		prose, err := os.ReadFile(proseFile(dataDir, ticket.ID))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		if strings.Contains(strings.ToLower(ticket.Title), pattern) || strings.Contains(strings.ToLower(string(prose)), pattern) {
			found = append(found, ticket)
		}
	}
	return found, nil
}

// writeSearch writes the same rows as dg list. No result is a useful answer,
// so it says so explicitly.
func writeSearch(out io.Writer, found []store.OpenTicket) {
	if len(found) == 0 {
		fmt.Fprintln(out, "No tickets match.")
		return
	}
	idWidth, projectWidth := ticketWidths(found)
	rowWidth := outputWidth(out)
	for _, ticket := range found {
		fmt.Fprintln(out, ticketRow(ticket, idWidth, projectWidth, rowWidth, string(ticket.Status)))
	}
}
