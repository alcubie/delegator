// Package cli provides the command layer of dg.
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

// mapCommand writes the component that contains one ticket. With no id it
// starts at the head of READY, as dg show does.
func mapCommand(dataDir, workDir string, cfg *config.Config) *cobra.Command {
	var projectDir string
	cmd := &cobra.Command{
		Use:   "map [id]",
		Short: mapShort,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withStore(dataDir, cfg, func(s *store.Store) error {
				id, err := resolveTicketID(s, cfg, args, workDir, projectDir)
				if err != nil {
					return err
				}
				tickets, err := mapTickets(s, id)
				if err != nil {
					return err
				}
				writeMap(cmd.OutOrStdout(), tickets)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&projectDir, "project", "",
		"the directory of the project whose first ready ticket to map.  Defaults to current working directory.")
	return cmd
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

// writeMap writes a component once. A ticket with more than one blocker is
// under its deepest blocker in topological order; the other blockers remain on
// its row, so a shared blocker neither disappears nor makes a second row.
func writeMap(out io.Writer, tickets []mappedTicket) {
	done := 0
	for _, ticket := range tickets {
		if ticket.Status == store.Done {
			done++
		}
	}
	fmt.Fprintf(out, "%d of %d done\n", done, len(tickets))

	depth := make(map[int64]int, len(tickets))
	for _, ticket := range tickets {
		parentDepth := -1
		parent := int64(0)
		for _, dependency := range ticket.DependsOn {
			if d, found := depth[dependency]; found && d > parentDepth {
				parent, parentDepth = dependency, d
			}
		}
		depth[ticket.ID] = parentDepth + 1

		var others []int64
		for _, dependency := range ticket.DependsOn {
			if dependency != parent {
				others = append(others, dependency)
			}
		}
		line := fmt.Sprintf("%s%s #%d %s", strings.Repeat("  ", depth[ticket.ID]), mapMark(ticket.Status), ticket.ID, ticket.Title)
		if len(others) > 0 {
			line += " (waits on " + ticketNames(others) + ")"
		}
		fmt.Fprintln(out, line)
	}
}
