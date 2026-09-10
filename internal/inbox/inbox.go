// Package inbox is the one list that the person examines. It answers which
// ticket is in which group, and in what order, and it writes no text.
//
// The rule that READY comes in the order of completion and QUEUED in the order
// of position lives here, and in one place only. A terminal is not the one
// interface that shows this list: internal/cli writes the text for a terminal,
// a TUI reads the same structure, and dg --json writes the structure itself. An
// order that lived in one of those would have to be written again in each other
// one, and the three would come apart.
//
// Nothing here knows that a terminal exists. It makes no string that a person
// reads, and it takes no width of a column.
package inbox

import (
	"cmp"
	"slices"
	"time"

	"github.com/alcubie/delegator/internal/store"
)

// Source returns the tickets that the inbox holds. The store has this method,
// and a test has its own.
type Source interface {
	OpenTickets() ([]store.OpenTicket, error)
	DoneTickets(since time.Time) ([]store.OpenTicket, error)
	IsQueueRunning() (bool, error)
}

// Inbox holds one group for each state that the person acts on. DONE holds the
// tickets that the person accepted lately, READY waits for the person, RUNNING
// has the one active run, and QUEUED waits for a run.
type Inbox struct {
	Done    []store.OpenTicket
	Ready   []store.OpenTicket
	Running []store.OpenTicket
	Queued  []store.OpenTicket

	// QueueRunning says whether the queue will start work. It is false after
	// dg pause, so a person who sees tickets waiting and no run knows why. The
	// name is not Running because Running is the group of tickets above.
	QueueRunning bool
}

// Get returns the inbox. since is the earliest completion that DONE shows: a
// ticket the person accepted before it has left the inbox. The caller gives
// the time rather than a window, because this package reads no clock.
func Get(source Source, since time.Time) (Inbox, error) {
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

	box.Done, err = source.DoneTickets(since)
	if err != nil {
		return Inbox{}, err
	}

	running, err := source.IsQueueRunning()
	if err != nil {
		return Inbox{}, err
	}
	box.QueueRunning = running

	slices.SortFunc(box.Done, byCompletion)
	slices.SortFunc(box.Ready, byCompletion)
	slices.SortFunc(box.Running, byID)
	slices.SortFunc(box.Queued, byPosition)
	return box, nil
}

// byCompletion orders the tickets by completion in ascending order. A ticket that
// becomes ready therefore goes at the end which keeps the ordering stable as
// new tickets are completed and added to the end of the list. DONE takes the
// same order for the same reason, and the newest finished ticket is at the
// end of it.
// The time holds one second and no part of a second, so two tickets can
// hold the same one, and the id then keeps the order stable.
func byCompletion(a, b store.OpenTicket) int {
	if by := a.Completed.Compare(b.Completed); by != 0 {
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

// byID orders the tickets by id, and the smallest id is first.
//
// Version 1 runs one ticket at a time, so RUNNING holds one ticket and this
// order shows after a fault that leaves two. It is here, and not the ORDER BY
// of the query, because a change to that query for another reason would move
// RUNNING and no test would say so.
func byID(a, b store.OpenTicket) int {
	return cmp.Compare(a.ID, b.ID)
}

// ReadySource is the part of Source that FirstReady reads. A caller that wants
// one ticket asks the store for the open tickets and nothing else.
type ReadySource interface {
	OpenTickets() ([]store.OpenTicket, error)
}

// FirstReady returns the ticket at the head of READY for the project at path,
// which is the ticket a person reviews next. found is false when that project
// has no ready ticket.
//
// The head is the ready ticket that byCompletion puts first, so it is the same
// ticket the inbox shows at the top of READY. A command that takes a ticket
// with no id calls this, and the rule of the order stays here.
func FirstReady(source ReadySource, path string) (ticket store.OpenTicket, found bool, err error) {
	tickets, err := source.OpenTickets()
	if err != nil {
		return store.OpenTicket{}, false, err
	}

	for _, t := range tickets {
		if t.Status != store.Ready || t.Project != path {
			continue
		}
		if !found || byCompletion(t, ticket) < 0 {
			ticket, found = t, true
		}
	}
	return ticket, found, nil
}
