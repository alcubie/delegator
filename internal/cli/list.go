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
	return rpcOperationCommand("list", cmd)
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

// listResultSchema describes the serialized []store.OpenTicket returned by
// list. Its legacy field names retain Go's exported-field casing; aligning
// them with the lowercase inbox rows is a future compatibility change. A query
// with no tickets is null because AllTickets returns a nil slice.
func listResultSchema() map[string]any {
	timestamp := map[string]any{"type": "string", "format": "date-time"}
	ticket := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ID":        map[string]any{"type": "integer", "minimum": 1},
			"Project":   map[string]any{"type": "string"},
			"Title":     map[string]any{"type": "string"},
			"Status":    map[string]any{"type": "string", "enum": []string{string(store.Queued), string(store.Running), string(store.Ready), string(store.Failed), string(store.Done), string(store.Cancelled)}},
			"Position":  map[string]any{"type": "integer", "minimum": 0},
			"Created":   timestamp,
			"Accepted":  timestamp,
			"Started":   timestamp,
			"Changed":   timestamp,
			"DependsOn": map[string]any{"type": []string{"array", "null"}, "items": map[string]any{"type": "integer", "minimum": 1}},
		},
		"required": []string{
			"ID", "Project", "Title", "Status", "Position", "Created",
			"Accepted", "Started", "Changed", "DependsOn",
		},
		"additionalProperties": true,
	}
	return map[string]any{
		"type":        []string{"array", "null"},
		"items":       map[string]any{"$ref": "#/definitions/ticket"},
		"definitions": map[string]any{"ticket": ticket},
	}
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
