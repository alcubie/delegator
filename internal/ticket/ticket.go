// Package ticket reads and writes tickets. A ticket is one Markdown file with a
// header, so a person can read it and change it with any editor.
package ticket

import "strings"

// Ticket is one item of work.
type Ticket struct {
	Title string
}

// Parse reads one ticket from the bytes of a file.
func Parse(data []byte) (*Ticket, error) {
	t := &Ticket{}
	for _, line := range strings.Split(string(data), "\n") {
		if value, found := strings.CutPrefix(line, "title: "); found {
			t.Title = value
		}
	}
	return t, nil
}
