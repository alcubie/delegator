// Shared row layout for the inbox and dg list.

package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/creack/pty"

	"github.com/alcubie/delegator/internal/store"
)

// defaultRowWidth preserves the original layout, matching dg show's rule,
// when neither terminal size nor COLUMNS is available.
const defaultRowWidth = ruleWidth

// outputWidth uses the terminal width or COLUMNS, which watch supplies to
// piped commands. Otherwise it uses defaultRowWidth.
func outputWidth(out io.Writer) int {
	f, ok := out.(*os.File)
	if !ok {
		return defaultRowWidth
	}
	_, columns, err := pty.Getsize(f)
	if err == nil && columns > 0 {
		return columns
	}
	fromEnvironment, err := strconv.ParseUint(os.Getenv("COLUMNS"), 10, 16)
	if err == nil && fromEnvironment > 0 {
		return int(fromEnvironment)
	}
	return defaultRowWidth
}

// timeGap is the space between the title of a row and the note at the right.
const timeGap = 2

// minTitleWidth keeps titles recognizable. Long project names can force rows
// beyond the requested width rather than squeeze titles below this minimum.
const minTitleWidth = 8

const ellipsis = "…"

// fit pads or truncates text to width runes, adding an ellipsis when
// truncated. It counts runes rather than bytes; this is not a display-cell
// width calculation.
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

// minIDWidth reserves space for ticket IDs so early digit-count changes do
// not shift every row.
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

// ticketRow returns a row of width columns: the id, the name of the project and
// the title, with note at the right of it. A row whose note is empty ends at
// its title.
func ticketRow(t store.OpenTicket, idWidth, projectWidth, width int, note string) string {
	left := fmt.Sprintf(" %*d %-*s  ",
		idWidth, t.ID, projectWidth, filepath.Base(t.Project))
	if note == "" {
		return left + t.Title
	}

	// Reserve the right edge for the note and give the remaining width to
	// the title.
	titleWidth := max(minTitleWidth,
		width-utf8.RuneCountInString(left)-timeGap-utf8.RuneCountInString(note))
	return fmt.Sprintf("%s%s%*s%s", left, fit(t.Title, titleWidth), timeGap, "", note)
}
