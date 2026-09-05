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
	"strings"
	"time"
	"unicode/utf8"

	"github.com/alcubie/delegator/internal/inbox"
	"github.com/alcubie/delegator/internal/store"
)

// emptyGroup is the line below the heading of a group that holds no ticket.
const emptyGroup = "  none"

// emptyInbox is the whole inbox of a person who has no ticket. Four headings
// with none below each of them say the same thing in eight lines, and a person
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

// groups returns each group of the inbox, always in one order. A group that
// holds no ticket keeps its place, so no heading moves below the eyes of the
// person who reads the inbox each day.
//
// DONE is first because it is the group that the person reads and leaves. What
// is left to do is below it, where the eyes stop.
func groups(box inbox.Inbox) []group {
	return []group{
		{"DONE", box.Done},
		{"READY", box.Ready},
		{"RUNNING", box.Running},
		{"QUEUED", box.Queued},
	}
}

// rowWidth is the width of a row that carries the duration of a run. It is the
// width of the rule of dg show, so the inbox and one ticket in full make the
// same shape on the screen.
const rowWidth = ruleWidth

// timeGap is the space between the title of a row and the duration of the run.
const timeGap = 2

// minTitleWidth is the least of a title that a row shows. A project with a
// name long enough to push the title below it makes the row wider than
// rowWidth instead, because a title cut to three characters names no ticket
// and the person can still read the duration.
const minTitleWidth = 8

// ellipsis ends a title that a row cut.
const ellipsis = "…"

// fit returns text at exactly width characters: it pads a short text with
// spaces, and it cuts a long one and puts an ellipsis at the end. The count is
// in characters and not in bytes, because the ellipsis takes three bytes and
// one column, and a title holds whatever the person wrote.
func fit(text string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= width {
		return text + strings.Repeat(" ", width-len(runes))
	}
	return string(runes[:width-1]) + ellipsis
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

// empty reports whether no group of the inbox holds a ticket. A person whose
// only tickets are in DONE has done work today, and the line that says how to
// make a ticket would take that away.
func empty(box inbox.Inbox) bool {
	for _, g := range groups(box) {
		if len(g.tickets) > 0 {
			return false
		}
	}
	return true
}

// writeInbox writes the inbox as of now. The time comes in rather than from
// the clock, because the row of a run holds the duration at the moment the
// text is written and a test has to name that moment.
func writeInbox(out io.Writer, box inbox.Inbox, mode colourMode, now time.Time) {
	fmt.Fprintln(out, statusLine(out, box, mode))
	if empty(box) {
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
			left := fmt.Sprintf(" %*d %-*s  ",
				idWidth, t.ID, projectWidth, filepath.Base(t.Project))

			// Only a ticket that runs now counts up. A ticket in READY holds
			// the start of the run that made it ready, and that run stopped.
			var since string
			if t.Status == store.Running {
				since = elapsed(t.Started, now)
			}
			if since == "" {
				fmt.Fprintln(out, left+t.Title)
				continue
			}

			// The duration ends the row at rowWidth, so the durations of two
			// runs are in one column. The title takes what is left, and it is
			// the field that gives way because it is the only one with no
			// width of its own.
			width := max(minTitleWidth,
				rowWidth-utf8.RuneCountInString(left)-timeGap-len(since))
			fmt.Fprintf(out, "%s%s%*s%s\n", left, fit(t.Title, width), timeGap, "", since)
		}
	}
}

// showInbox reads the tickets and writes the inbox. done is how far back DONE
// reaches, and it comes from the config file.
func showInbox(out io.Writer, dataDir string, mode colourMode, done time.Duration) error {
	return store.With(dataDir, func(s *store.Store) error {

		now := time.Now()
		box, err := inbox.Get(s, now.Add(-done))
		if err != nil {
			return err
		}
		writeInbox(out, box, mode, now)
		return nil
	})
}
