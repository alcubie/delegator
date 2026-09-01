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

// emptyInbox is the whole inbox of a person who has no ticket. Three headings
// with none below each of them say the same thing in six lines, and a person
// who has no ticket has not made one yet, so the line says what makes one.
const emptyInbox = "There are no active tickets. Use `dg ticket` to add."

// group is one heading of the inbox and the tickets below it.
type group struct {
	heading string
	tickets []store.OpenTicket
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
	if len(box.Ready) == 0 && len(box.Running) == 0 && len(box.Queued) == 0 {
		fmt.Fprintln(out, emptyInbox)
		return
	}

	idWidth, projectWidth := widths(box)
	for _, g := range groups(box) {
		fmt.Fprintln(out, g.heading)
		if len(g.tickets) == 0 {
			fmt.Fprintln(out, emptyGroup)
			continue
		}
		for _, t := range g.tickets {
			fmt.Fprintf(out, " %*d %-*s  %s\n",
				idWidth, t.ID, projectWidth,
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
