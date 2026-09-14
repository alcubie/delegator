// Package inbox is the one list that the person examines. It answers which
// ticket is in which group, and in what order, and it writes no text.
//
// The rule that READY and QUEUED come in the order of position, and DONE in
// the order of acceptance, lives here, and in one place only. A terminal is not
// the one interface that shows this list: internal/cli writes the text for a
// terminal, a TUI reads the same structure, and dg --json writes it itself. An
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
// has the one active run, FAILED holds the runs that stopped without a report,
// and QUEUED waits for a run.
type Inbox struct {
	Done    []store.OpenTicket
	Ready   []store.OpenTicket
	Running []store.OpenTicket
	Failed  []store.OpenTicket
	Queued  []store.OpenTicket

	// QueueRunning says whether the queue will start work. It is false after
	// dg pause, so a person who sees tickets waiting and no run knows why. The
	// name is not Running because Running is the group of tickets above.
	QueueRunning bool
}

// Get returns the inbox. since is the earliest acceptance that DONE shows: a
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
		case store.Failed:
			box.Failed = append(box.Failed, ticket)
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

	slices.SortFunc(box.Done, byAcceptance)
	slices.SortFunc(box.Ready, byPosition)
	slices.SortFunc(box.Running, byID)
	slices.SortFunc(box.Failed, byChange)
	slices.SortFunc(box.Queued, byPosition)
	return box, nil
}

// byAcceptance orders the tickets by acceptance in ascending order. A ticket
// that the person accepts therefore goes at the end, which keeps the order
// stable as tickets are accepted, and the one accepted last is at the end of
// DONE.
// The time holds one second and no part of a second, so two tickets can
// hold the same one, and the id then keeps the order stable.
func byAcceptance(a, b store.OpenTicket) int {
	if by := a.Accepted.Compare(b.Accepted); by != 0 {
		return by
	}
	return cmp.Compare(a.ID, b.ID)
}

// byPosition orders the tickets by position in ascending order. A ticket that
// enters a group goes at the end of it, and dg move takes it from there, so
// the order is the one the person last set.
// This assumes the position of each ticket is unique which is enforced by the
// database for the tickets of one group.
func byPosition(a, b store.OpenTicket) int {
	return cmp.Compare(a.Position, b.Position)
}

// byChange orders the tickets by the time of their last change of state in
// ascending order. The ticket that failed first therefore goes at the top of
// FAILED, and the one that failed last is at the end, so the failure that has
// waited longest is the one the person sees first.
// The time holds one second and no part of a second, so two tickets can
// hold the same one, and the id then keeps the order stable.
func byChange(a, b store.OpenTicket) int {
	if by := a.Changed.Compare(b.Changed); by != 0 {
		return by
	}
	return cmp.Compare(a.ID, b.ID)
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

// FirstReady returns the ticket at the head of READY for the project at path,
// which is the ticket a person reviews next. found is false when that project
// has no ready ticket. A command that takes a ticket with no id calls it.
//
// Get has already put READY in its order, so the first ticket of the project is
// the head of it. Nothing here reads a column or orders a ticket: the head of
// READY is the ticket at the top of READY that the person is looking at, and a
// second walk of the tickets could disagree with the list the person can see.
func (b Inbox) FirstReady(path string) (store.OpenTicket, bool) {
	for _, t := range b.Ready {
		if t.Project == path {
			return t, true
		}
	}
	return store.OpenTicket{}, false
}
