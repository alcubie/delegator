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
	ID     int
	Title  string
	Flags  string
	Body   string
}

// Parse reads one ticket from the bytes of a file. The header is between the
// first line and the next line that holds "---". The body comes after it.
func Parse(data []byte) (*Ticket, error) {
	t := &Ticket{}
	lines := strings.Split(string(data), "\n")

	headerEnd := len(lines)
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			headerEnd = i
			break
		}
	}
	if headerEnd < len(lines) {
		rest := strings.Join(lines[headerEnd+1:], "\n")
		t.Body = strings.TrimLeft(rest, "\n")
	}

	for _, line := range unfold(lines[1:headerEnd]) {
		key, value, found := strings.Cut(line, ": ")
		if !found {
			continue
		}
		var err error
		switch key {
		case "schema":
			t.Schema, err = strconv.Atoi(value)
		case "id":
			t.ID, err = strconv.Atoi(value)
		case "title":
			t.Title = value
		case "flags":
			t.Flags = value
		}
		if err != nil {
			return nil, err
		}
	}
	return t, nil
}

// unfold joins each continuation line to the line above it. A continuation line
// starts with a space and has no key of its own.
func unfold(lines []string) []string {
	var out []string
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		if len(out) > 0 && trimmed != "" && trimmed != line {
			out[len(out)-1] += " " + trimmed
			continue
		}
		out = append(out, line)
	}
	return out
}
