// Package inbox groups and sorts tickets for display. It defines the shared
// ordering for terminal and JSON output without formatting text or reading
// the clock.
package inbox

import (
	"cmp"
	"slices"
	"time"

	"github.com/alcubie/delegator/internal/store"
)

// Source provides tickets and queue state to the inbox.
type Source interface {
	OpenTickets() ([]store.OpenTicket, error)
	DoneTickets(since time.Time) ([]store.OpenTicket, error)
	IsQueueRunning() (bool, error)
}

// Inbox groups tickets by status: recently accepted work in Done, work
// awaiting review in Ready, active work in Running, unsuccessful runs in
// Failed, and pending work in Queued.
type Inbox struct {
	Done    []store.OpenTicket
	Ready   []store.OpenTicket
	Running []store.OpenTicket
	Failed  []store.OpenTicket
	Queued  []store.OpenTicket

	// QueueRunning reports whether the queue can start new work. It is
	// false after dg pause.
	QueueRunning bool
}

// Get returns the grouped, sorted inbox. Done includes tickets accepted at or
// after since. The caller supplies the cutoff so this package need not read
// the clock.
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

// byAcceptance sorts oldest acceptances first, placing newly accepted tickets
// at the end of DONE. IDs break ties because timestamps have one-second
// resolution.
func byAcceptance(a, b store.OpenTicket) int {
	if by := a.Accepted.Compare(b.Accepted); by != 0 {
		return by
	}
	return cmp.Compare(a.ID, b.ID)
}

// byPosition sorts tickets in the order set by dg move. New entries go last;
// the database enforces unique positions within each group.
func byPosition(a, b store.OpenTicket) int {
	return cmp.Compare(a.Position, b.Position)
}

// byChange sorts oldest state changes first, showing the longest-waiting
// failures first. IDs break ties because timestamps have one-second
// resolution.
func byChange(a, b store.OpenTicket) int {
	if by := a.Changed.Compare(b.Changed); by != 0 {
		return by
	}
	return cmp.Compare(a.ID, b.ID)
}

// byID sorts running tickets by ID, independently of the database query
// order.
func byID(a, b store.OpenTicket) int {
	return cmp.Compare(a.ID, b.ID)
}

// FirstReady returns the first ready ticket for the project at path, or
// found=false if there is none. Commands without an explicit ticket ID use
// this to select the same ticket shown first in the inbox.
func (b Inbox) FirstReady(path string) (store.OpenTicket, bool) {
	for _, t := range b.Ready {
		if t.Project == path {
			return t, true
		}
	}
	return store.OpenTicket{}, false
}
