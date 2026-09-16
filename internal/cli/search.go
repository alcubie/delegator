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

// searchResult is one ticket that search found. Prose holds the lines that
// matched in its file; a title match alone leaves it empty.
type searchResult struct {
	Ticket store.OpenTicket
	Prose  []string
}

// searchCommand finds pattern in a title or in the Markdown prose. --project
// takes the same directory as it does on dg show and dg list.
func searchCommand(dataDir, workDir string, cfg *config.Config) *cobra.Command {
	var projectDir string
	cmd := &cobra.Command{
		Use:   "search <pattern>",
		Short: searchShort,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			found, err := searchTickets(dataDir, workDir, cfg, projectDir, args[0])
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
func searchTickets(dataDir, workDir string, cfg *config.Config, projectDir, pattern string) ([]searchResult, error) {
	tickets, err := listTickets(dataDir, workDir, cfg, projectDir)
	if err != nil {
		return nil, err
	}

	pattern = strings.ToLower(pattern)
	found := make([]searchResult, 0)
	for _, ticket := range tickets {
		prose, err := os.ReadFile(proseFile(dataDir, ticket.ID))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		lines := matchingLines(string(prose), pattern)
		if strings.Contains(strings.ToLower(ticket.Title), pattern) || len(lines) > 0 {
			found = append(found, searchResult{Ticket: ticket, Prose: lines})
		}
	}
	return found, nil
}

// matchingLines returns each line that holds pattern. A line is the useful
// part of a prose match: it tells the person why this ticket was returned.
func matchingLines(prose, pattern string) []string {
	var matched []string
	for _, line := range strings.Split(prose, "\n") {
		if strings.Contains(strings.ToLower(line), pattern) {
			matched = append(matched, line)
		}
	}
	return matched
}

// writeSearch writes the same rows as dg list, and indents the prose lines
// below their ticket. No result is a useful answer, so it says so explicitly.
func writeSearch(out io.Writer, found []searchResult) {
	if len(found) == 0 {
		fmt.Fprintln(out, "No tickets match.")
		return
	}
	tickets := make([]store.OpenTicket, 0, len(found))
	for _, result := range found {
		tickets = append(tickets, result.Ticket)
	}
	idWidth, projectWidth := ticketWidths(tickets)
	for _, result := range found {
		fmt.Fprintln(out, ticketRow(result.Ticket, idWidth, projectWidth, string(result.Ticket.Status)))
		for _, line := range result.Prose {
			fmt.Fprintf(out, "  %s\n", line)
		}
	}
}
