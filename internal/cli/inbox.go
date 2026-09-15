// The text of the inbox for a terminal. internal/inbox answers which ticket is
// in which group and in what order, and this file answers how that looks: the
// headings, the note at the right of a row, and the line below a group that
// holds no ticket. row.go holds the shape of a row itself.
//
// The two are apart so that a second interface can show the same list its own
// way. This file therefore takes an inbox.Inbox and makes text from it, and it
// decides nothing about which ticket goes where.

package cli

import (
	"fmt"
	"io"
	"time"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/inbox"
	"github.com/alcubie/delegator/internal/store"
)

// emptyGroup is the line below the heading of a group that holds no ticket.
const emptyGroup = "  none"

// emptyInbox is the whole inbox of a person who has no ticket. A heading with
// none below it for each group says the same thing over and over, and a person
// who has no ticket has not made one yet, so the line says what makes one.
const emptyInbox = "There are no active tickets. Use `dg ticket` to add."

// The first line of the inbox says whether the queue will start work. It is
// always there, so a person never has to know what the absence of a line
// means. The paused form names no command to resume, because dg start does not
// exist yet.
const (
	statusRunning = "Status: Running"
	statusPaused  = "Status: Paused"
)

// statusLine returns the first line of the inbox for the state of the queue.
// The mode says whether the word is coloured: by default only when out is a
// terminal, so a pipe, a script or a test sees the plain text.
func statusLine(out io.Writer, box inbox.Inbox, mode colourMode) string {
	if box.QueueRunning {
		return colour(mode.on(out), green, statusRunning)
	}
	return colour(mode.on(out), yellow, statusPaused)
}

// group is one heading of the inbox and the tickets below it.
type group struct {
	heading string
	tickets []store.OpenTicket
}

// doneHeading returns the heading of DONE for a window of that length. The
// group holds what the person accepted inside the window and nothing older, so
// the heading says how far back it reaches: a DONE with one ticket in it then
// reads as the work of the last day rather than as all the work there has ever
// been, and a DONE with none in it reads as a window that ends before the work.
//
// The window is whole hours because the config file asks for hours, and the
// text says the same unit as the key the person edits.
func doneHeading(window time.Duration) string {
	return fmt.Sprintf("DONE (last %dh)", int(window.Hours()))
}

// groups returns each group of the inbox, always in one order. A group that
// holds no ticket keeps its place, so no heading moves below the eyes of the
// person who reads the inbox each day. done is how far back DONE reaches.
//
// DONE is first because it is the group that the person reads and leaves. What
// is left to do is below it, where the eyes stop.
//
// FAILED is the one group that goes when it is empty. The other four hold the
// work of a day that went as it should, and a person reads them each time;
// FAILED holds only what went wrong, so an empty one is the normal case and a
// heading for it is a line that says nothing on nearly every run. Gone, the
// heading means something whenever it is there.
func groups(box inbox.Inbox, done time.Duration) []group {
	gs := []group{
		{doneHeading(done), box.Done},
		{"READY", box.Ready},
		{"RUNNING", box.Running},
	}
	if len(box.Failed) > 0 {
		gs = append(gs, group{"FAILED", box.Failed})
	}
	return append(gs, group{"QUEUED", box.Queued})
}

// widths returns the width of the column of ids and the width of the column of
// projects. One width holds for each group, so the titles of two groups are
// below one another.
func widths(gs []group) (id, project int) {
	id = minIDWidth
	for _, g := range gs {
		gid, gproject := ticketWidths(g.tickets)
		id, project = max(id, gid), max(project, gproject)
	}
	return id, project
}

// empty reports whether no group of the inbox holds a ticket. A person whose
// only tickets are in DONE has done work today, and the line that says how to
// make a ticket would take that away.
func empty(gs []group) bool {
	for _, g := range gs {
		if len(g.tickets) > 0 {
			return false
		}
	}
	return true
}

// rowNote returns the text at the right of one row, and the empty string for a
// row that ends at its title.
//
// Only a ticket that runs now counts up. A ticket in READY holds the start of
// the run that made it ready, and that run stopped. A queued ticket that
// depends on another names the tickets it depends on, because a person who sees
// a ticket at the top of the queue and no run needs to know that the queue is
// passing it over on purpose.
func rowNote(t store.OpenTicket, now time.Time) string {
	switch t.Status {
	case store.Running:
		return elapsed(t.Started, now)
	case store.Queued:
		return dependsOnText(t.DependsOn)
	}
	return ""
}

// dependsOnText names the tickets that one ticket depends on. A ticket that
// depends on none gives the empty string, and its row ends at the title.
func dependsOnText(ids []int64) string {
	if len(ids) == 0 {
		return ""
	}
	return "depends on " + ticketNames(ids)
}

// writeInbox writes the inbox as of now. The time comes in rather than from
// the clock, because the row of a run holds the duration at the moment the
// text is written and a test has to name that moment. done is the window of
// DONE, which the heading of that group says.
func writeInbox(out io.Writer, box inbox.Inbox, mode colourMode, now time.Time, done time.Duration) {
	gs := groups(box, done)
	fmt.Fprintln(out, statusLine(out, box, mode))
	if empty(gs) {
		fmt.Fprintln(out, emptyInbox)
		return
	}

	idWidth, projectWidth := widths(gs)
	for _, g := range gs {
		fmt.Fprintln(out, g.heading)
		if len(g.tickets) == 0 {
			fmt.Fprintln(out, emptyGroup)
			continue
		}
		for _, t := range g.tickets {
			fmt.Fprintln(out, ticketRow(t, idWidth, projectWidth, rowNote(t, now)))
		}
	}
}

// showInbox reads the tickets into the value that dg writes. How far back DONE
// reaches comes from the config file.
func showInbox(dataDir string, cfg *config.Config) (inboxJSON, error) {
	done := cfg.DoneWindow()
	var value inboxJSON
	err := withStore(dataDir, cfg, func(s *store.Store) error {

		now := time.Now()
		box, err := inbox.Get(s, now.Add(-done))
		if err != nil {
			return err
		}
		value = inboxValue(box, now, done)
		return nil
	})
	return value, err
}
