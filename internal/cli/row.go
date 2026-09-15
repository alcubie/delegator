// The shape of one row of a list of tickets: the width of each column and the
// text of the row itself. The inbox and dg list are two lists of the same
// tickets, and they write a row the one way, so a person who reads one of them
// reads the other without learning it again.

package cli

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/alcubie/delegator/internal/store"
)

// rowWidth is the width of a row that carries a note. It is the width of the
// rule of dg show, so the inbox and one ticket in full make the same shape on
// the screen.
const rowWidth = ruleWidth

// timeGap is the space between the title of a row and the note at the right.
const timeGap = 2

// minTitleWidth is the least of a title that a row shows. A project with a
// name long enough to push the title below it makes the row wider than
// rowWidth instead, because a title cut to three characters names no ticket
// and the person can still read the note.
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

// ticketWidths returns the width of the column of ids and the width of the
// column of projects for one list of tickets.
func ticketWidths(tickets []store.OpenTicket) (id, project int) {
	id = minIDWidth
	for _, t := range tickets {
		if w := len(strconv.FormatInt(t.ID, 10)); w > id {
			id = w
		}
		if w := len(filepath.Base(t.Project)); w > project {
			project = w
		}
	}
	return id, project
}

// ticketRow returns the row of one ticket: the id, the name of the project and
// the title, with note at the right of it. A row whose note is empty ends at
// its title.
func ticketRow(t store.OpenTicket, idWidth, projectWidth int, note string) string {
	left := fmt.Sprintf(" %*d %-*s  ",
		idWidth, t.ID, projectWidth, filepath.Base(t.Project))
	if note == "" {
		return left + t.Title
	}

	// The note ends the row at rowWidth, so the notes of two rows are in one
	// column. The title takes what is left, and it is the field that gives way
	// because it is the only one with no width of its own.
	width := max(minTitleWidth,
		rowWidth-utf8.RuneCountInString(left)-timeGap-utf8.RuneCountInString(note))
	return fmt.Sprintf("%s%s%*s%s", left, fit(t.Title, width), timeGap, "", note)
}
