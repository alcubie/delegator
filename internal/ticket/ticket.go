// Package ticket reads and writes tickets. A ticket is one Markdown file with a
// header, so a person can read it and change it with any editor.
package ticket

import (
	"strconv"
	"strings"
)

// Ticket is one item of work.
type Ticket struct {
	Schema int
	Title  string
	Body   string
}

// Parse reads one ticket from the bytes of a file. The header is between the
// first line and the next line that holds "---". The body comes after it.
func Parse(data []byte) (*Ticket, error) {
	t := &Ticket{}
	lines := strings.Split(string(data), "\n")
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			rest := strings.Join(lines[i+1:], "\n")
			t.Body = strings.TrimLeft(rest, "\n")
			break
		}
		if value, found := strings.CutPrefix(lines[i], "schema: "); found {
			num, err := strconv.Atoi(value)
			if err != nil {
				return nil, err
			}
			t.Schema = num
		}
		if value, found := strings.CutPrefix(lines[i], "title: "); found {
			t.Title = value
		}
	}
	return t, nil
}
