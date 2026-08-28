// Package inbox is the one list that the person examines. It holds the
// groups and their order, and it makes no text: internal/cli makes the text
// for a terminal, and another interface can make its own from the same
// structure.
package inbox

import (
	"cmp"
	"slices"

	"github.com/alcubie/delegator/internal/store"
)

// Source returns the tickets that the inbox holds. The store has this method,
// and a test has its own.
type Source interface {
	OpenTickets() ([]store.OpenTicket, error)
}

// Inbox holds one group for each state that the person acts on. READY waits for
// the person, RUNNING has the one active run, and QUEUED waits for a run.
type Inbox struct {
	Ready   []store.OpenTicket
	Running []store.OpenTicket
	Queued  []store.OpenTicket
}

// Get returns the inbox.
func Get(source Source) (Inbox, error) {
	tickets, err := source.OpenTickets()
	if err != nil {
		return Inbox{}, err
	}

	var box Inbox
	for _, ticket := range tickets {
		switch ticket.Status {
		case store.Ready:
			box.Ready = append(box.Ready, ticket)
		case store.Running:
			box.Running = append(box.Running, ticket)
		case store.Queued:
			box.Queued = append(box.Queued, ticket)
		}
	}

	slices.SortFunc(box.Ready, byCompletion)
	slices.SortFunc(box.Queued, byPosition)
	return box, nil
}

// byCompletion orders the tickets by completion in ascending order. A ticket that
// becomes ready therefore goes at the end which keeps the ordering stable as
// new tickets are completed and added to the end of the list.
// The time holds one second and no part of a second, so two tickets can
// hold the same one, and the id then keeps the order stable.
func byCompletion(a, b store.OpenTicket) int {
	if by := cmp.Compare(a.Completed, b.Completed); by != 0 {
		return by
	}
	return cmp.Compare(a.ID, b.ID)
}

// byPosition orders the tickets by position in ascending order.
// This assumes the position of each ticket is unique which is enforced by the
// database for queued tickets.
func byPosition(a, b store.OpenTicket) int {
	return cmp.Compare(a.Position, b.Position)
}
