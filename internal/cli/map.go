package cli

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/store"
)

// mapShort is the line the help gives for dg map.
const mapShort = "Show the dependency graph of a ticket"

// mappedTicket is one ticket in a dependency component. DependsOn holds every
// dependency, including ones that are already done: the map is the record of
// a piece of work, not just the work that remains blocked.
type mappedTicket struct {
	store.Ticket
	DependsOn []int64
}

// mapTicketJSON is one ticket of the structured map result. depends_on is
// always an array and holds done dependencies too, as the text tree does.
type mapTicketJSON struct {
	ID        int64   `json:"id"`
	Title     string  `json:"title"`
	Status    string  `json:"status"`
	Project   string  `json:"project"`
	DependsOn []int64 `json:"depends_on"`
}

// mapJSON is the structured map result. tickets keeps the order that
// topologicalMap gives, so a consumer can draw the graph top down.
type mapJSON struct {
	Tickets []mapTicketJSON `json:"tickets"`
}

func mapValue(tickets []mappedTicket) mapJSON {
	value := mapJSON{Tickets: make([]mapTicketJSON, 0, len(tickets))}
	for _, ticket := range tickets {
		value.Tickets = append(value.Tickets, mapTicketJSON{
			ID:        ticket.ID,
			Title:     ticket.Title,
			Status:    string(ticket.Status),
			Project:   ticket.Project.Path,
			DependsOn: append([]int64{}, ticket.DependsOn...),
		})
	}
	return value
}

// mapResultSchema describes mapJSON for RPC discovery. Additional properties
// stay valid because adding fields does not change jsonSchema.
func mapResultSchema() map[string]any {
	ticket := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":      map[string]any{"type": "integer", "minimum": 1},
			"title":   map[string]any{"type": "string"},
			"status":  ticketStatusSchema(),
			"project": map[string]any{"type": "string"},
			"depends_on": map[string]any{
				"description": "Every prerequisite ticket ID, done ones included, in ascending order.",
				"type":        "array",
				"items":       map[string]any{"type": "integer", "minimum": 1},
			},
		},
		"required":             []string{"id", "title", "status", "project", "depends_on"},
		"additionalProperties": true,
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tickets": map[string]any{
				"description": "Every ticket of the connected dependency graph, each blocker " +
					"before the tickets that wait on it and ties broken by ascending ID.",
				"type":  "array",
				"items": ticket,
			},
		},
		"required":             []string{"tickets"},
		"additionalProperties": true,
	}
}

// mapCommand writes the component that contains one ticket. With no id it
// starts at the head of READY, as dg show does.
func mapCommand(dataDir *string, workDir string, cfg *config.Config) *cobra.Command {
	var projectDir string
	var mermaid bool
	cmd := &cobra.Command{
		Use:   "map [id]",
		Short: mapShort,
		Long: "Show the connected dependency graph containing a ticket. With no ID, start " +
			"from the first ready ticket for the selected project.",
		Example: `  dg ticket map 42
  dg ticket map
  dg ticket map 42 --mermaid`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withStore(*dataDir, cfg, func(s *store.Store) error {
				id, err := resolveTicketID(s, cfg, args, workDir, projectDir)
				if err != nil {
					return err
				}
				tickets, err := mapTickets(s, id)
				if err != nil {
					return err
				}
				return writeValue(cmd.OutOrStdout(), mapValue(tickets), false, func(out io.Writer) {
					if mermaid {
						writeMermaid(out, tickets)
					} else {
						writeMap(out, tickets)
					}
				})
			})
		},
	}
	cmd.Flags().StringVar(&projectDir, "project", "",
		"select the project whose first ready ticket to map when ID is omitted (default: current working directory)")
	cmd.Flags().BoolVar(&mermaid, "mermaid", false,
		"write a Mermaid flowchart instead of the default text tree")
	return rpcOperationCommand("map", cmd)
}

// mapTickets walks both ends of every dependency link from id. The store keeps
// links in both directions as separate queries, so a ticket named in the
// middle reaches the same component as either end.
func mapTickets(s *store.Store, id int64) ([]mappedTicket, error) {
	seen := make(map[int64]bool)
	byID := make(map[int64]mappedTicket)
	pending := []int64{id}
	for len(pending) > 0 {
		id := pending[0]
		pending = pending[1:]
		if seen[id] {
			continue
		}
		seen[id] = true

		ticket, err := s.Ticket(id)
		if err != nil {
			return nil, err
		}
		dependencies, err := s.Dependencies(id)
		if err != nil {
			return nil, err
		}
		dependents, err := s.Dependents(id)
		if err != nil {
			return nil, err
		}
		byID[id] = mappedTicket{Ticket: ticket, DependsOn: dependencies}
		pending = append(pending, dependencies...)
		pending = append(pending, dependents...)
	}

	return topologicalMap(byID)
}

// topologicalMap puts blockers before the tickets that wait on them. Among
// tickets that can happen at the same time, id keeps the output stable.
func topologicalMap(byID map[int64]mappedTicket) ([]mappedTicket, error) {
	incoming := make(map[int64]int, len(byID))
	dependents := make(map[int64][]int64, len(byID))
	for id, ticket := range byID {
		for _, dependency := range ticket.DependsOn {
			if _, found := byID[dependency]; !found {
				continue
			}
			incoming[id]++
			dependents[dependency] = append(dependents[dependency], id)
		}
	}

	ready := make([]int64, 0, len(byID))
	for id := range byID {
		if incoming[id] == 0 {
			ready = append(ready, id)
		}
	}
	sort.Slice(ready, func(i, j int) bool { return ready[i] < ready[j] })

	ordered := make([]mappedTicket, 0, len(byID))
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		ordered = append(ordered, byID[id])
		for _, dependent := range dependents[id] {
			incoming[dependent]--
			if incoming[dependent] == 0 {
				ready = append(ready, dependent)
			}
		}
		sort.Slice(ready, func(i, j int) bool { return ready[i] < ready[j] })
	}
	if len(ordered) != len(byID) {
		return nil, errors.New("dependency graph has a cycle")
	}
	return ordered, nil
}

// mapMark is the compact state that starts a map row.
func mapMark(status store.TicketStatus) string {
	switch status {
	case store.Done:
		return "✓"
	case store.Cancelled:
		return "×"
	case store.Running:
		return "→"
	case store.Ready:
		return "•"
	case store.Failed:
		return "!"
	default:
		return "○"
	}
}

// writeMap builds a tree from topologically ordered tickets, then writes each
// branch together. Each ticket goes under its deepest blocker, breaking ties
// by higher id; the other blockers remain on its row.
func writeMap(out io.Writer, tickets []mappedTicket) {
	done := 0
	depths := make(map[int64]int, len(tickets))
	children := make(map[int64][]mappedTicket)
	for _, ticket := range tickets {
		if ticket.Status == store.Done {
			done++
		}
		depth := 0
		parent := int64(0)
		for _, dependency := range ticket.DependsOn {
			if blockerDepth, found := depths[dependency]; found {
				if candidateDepth := blockerDepth + 1; candidateDepth > depth ||
					(candidateDepth == depth && dependency > parent) {
					parent, depth = dependency, candidateDepth
				}
			}
		}
		depths[ticket.ID] = depth
		children[parent] = append(children[parent], ticket)
	}
	fmt.Fprintf(out, "%d of %d done\n", done, len(tickets))

	var writeBranch func(int64, int)
	writeBranch = func(parent int64, depth int) {
		for _, ticket := range children[parent] {
			var others []int64
			for _, dependency := range ticket.DependsOn {
				if dependency != parent {
					others = append(others, dependency)
				}
			}
			line := fmt.Sprintf("%s%s #%d %s", strings.Repeat("  ", depth), mapMark(ticket.Status), ticket.ID, ticket.Title)
			if len(others) > 0 {
				line += " (waits on " + ticketNames(others) + ")"
			}
			fmt.Fprintln(out, line)
			writeBranch(ticket.ID, depth+1)
		}
	}
	writeBranch(0, 0)
}
