// The text of the inbox for a terminal. internal/inbox answers which ticket is
// in which group and in what order, and this file answers how that looks: the
// headings, the width of each column, and the line below a group that holds no
// ticket.
//
// The two are apart so that a second interface can show the same list its own
// way. This file therefore takes an inbox.Inbox and makes text from it, and it
// decides nothing about which ticket goes where.

package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"strconv"

	"github.com/alcubie/delegator/internal/inbox"
	"github.com/alcubie/delegator/internal/store"
)

// emptyGroup is the line below the heading of a group that holds no ticket.
const emptyGroup = "  none"

// noFlags is what the agent writes when each item went as the ticket said. It
// is a complete answer, and not an empty one.
const noFlags = "none"

// flagMark stands in the row of a ticket that has a problem, and a space stands
// in the row of a ticket that has none.
//
// The row holds the mark and not the flags. The flags take up to 240
// characters, and one of those wraps a row of a terminal three times, so a
// person can no longer read the inbox as a list. The mark answers what the
// inbox is for: which tickets have a problem, and which the person can accept
// with no more commands. dg show gives the words.
const flagMark = "⚠"

// group is one heading of the inbox and the tickets below it.
type group struct {
	heading string
	tickets []store.OpenTicket
}

// flagsText returns the mark for the flags of one ticket. A ticket that never
// went through dg finish holds no flags, and it reads as a ticket that had no
// problem.
//
// Each group asks this, and not READY alone. dg revise takes the flags away
// when it puts a ticket back in the queue, because the person read them and is
// giving the work again, so a ticket that waits holds no mark. A ticket that
// holds flags in another group therefore has a problem that a person must see.
func flagsText(flags string) string {
	if flags == "" || flags == noFlags {
		return " "
	}
	return flagMark
}

// groups returns each group of the inbox, always in one order. A group that
// holds no ticket keeps its place, so no heading moves below the eyes of the
// person who reads the inbox each day.
func groups(box inbox.Inbox) []group {
	return []group{
		{"READY", box.Ready},
		{"RUNNING", box.Running},
		{"QUEUED", box.Queued},
	}
}

// minIDWidth is the smallest width of the column of ids. A column that grows
// with the largest id would move each row to the right at the id 10, and the
// inbox is a list that a person reads each day.
const minIDWidth = 2

// widths returns the width of the column of ids and the width of the column of
// projects. One width holds for each group, so the titles of two groups are
// below one another.
func widths(box inbox.Inbox) (id, project int) {
	id = minIDWidth
	for _, g := range groups(box) {
		for _, t := range g.tickets {
			if w := len(strconv.FormatInt(t.ID, 10)); w > id {
				id = w
			}
			if w := len(filepath.Base(t.Project)); w > project {
				project = w
			}
		}
	}
	return id, project
}

func writeInbox(out io.Writer, box inbox.Inbox) {
	idWidth, projectWidth := widths(box)
	for _, g := range groups(box) {
		fmt.Fprintln(out, g.heading)
		if len(g.tickets) == 0 {
			fmt.Fprintln(out, emptyGroup)
			continue
		}
		for _, t := range g.tickets {
			fmt.Fprintf(out, " %*d %s  %-*s  %s\n",
				idWidth, t.ID, flagsText(t.Flags), projectWidth,
				filepath.Base(t.Project), t.Title)
		}
	}
}

// showInbox reads the tickets and writes the inbox.
func showInbox(out io.Writer, dataDir string) error {
	s, err := store.Open(dataDir)
	if err != nil {
		return err
	}
	defer s.Close()

	box, err := inbox.Get(s)
	if err != nil {
		return err
	}
	writeInbox(out, box)
	return nil
}
