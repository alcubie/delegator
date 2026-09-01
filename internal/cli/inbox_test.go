package cli

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/inbox"
	"github.com/alcubie/delegator/internal/store"
)

// render writes one inbox and returns each line of it.
func render(t *testing.T, box inbox.Inbox) []string {
	t.Helper()
	var out bytes.Buffer
	writeInbox(&out, box)
	return strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
}

func TestWriteInboxHoldsTheThreeGroupsInOneOrder(t *testing.T) {
	box := inbox.Inbox{
		Ready:   []store.OpenTicket{{ID: 4, Project: "/projects/one", Title: "the first title"}},
		Running: []store.OpenTicket{{ID: 9, Project: "/projects/one", Title: "the second title"}},
		Queued:  []store.OpenTicket{{ID: 11, Project: "/projects/a-longer-name", Title: "the third title"}},
	}

	var headings []int
	lines := render(t, box)
	for i, line := range lines {
		if line == "READY" || line == "RUNNING" || line == "QUEUED" {
			headings = append(headings, i)
		}
	}
	if len(headings) != 3 {
		t.Fatalf("the inbox holds %d headings, want 3:\n%s", len(headings), strings.Join(lines, "\n"))
	}
	for i, want := range []string{"READY", "RUNNING", "QUEUED"} {
		if got := lines[headings[i]]; got != want {
			t.Errorf("heading %d is %q, want %q", i, got, want)
		}
	}
}

// A group that holds no ticket keeps its heading, so no group moves below the
// eyes of the person, and a line says that the group is empty.
func TestWriteInboxKeepsAnEmptyGroup(t *testing.T) {
	box := inbox.Inbox{
		Queued: []store.OpenTicket{{ID: 3, Project: "/projects/one", Title: "the title"}},
	}

	want := []string{
		"READY",
		"  none",
		"RUNNING",
		"  none",
		"QUEUED",
		"  3 one  the title",
	}
	got := render(t, box)
	if len(got) != len(want) {
		t.Fatalf("the inbox is\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d is %q, want %q", i, got[i], want[i])
		}
	}
}

// The id is right of its column and each project takes the same width, so the
// titles of two groups are below one another.
func TestWriteInboxPutsTheColumnsTogether(t *testing.T) {
	box := inbox.Inbox{
		Ready:  []store.OpenTicket{{ID: 4, Project: "/projects/one", Title: "the first title"}},
		Queued: []store.OpenTicket{{ID: 11, Project: "/projects/a-longer-name", Title: "the third title"}},
	}

	got := render(t, box)
	want := []string{
		"READY",
		"  4 one            the first title",
		"RUNNING",
		"  none",
		"QUEUED",
		" 11 a-longer-name  the third title",
	}
	if len(got) != len(want) {
		t.Fatalf("the inbox is\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d is %q, want %q", i, got[i], want[i])
		}
	}
}

// The project of a ticket is a path, and the inbox shows the name at the end of
// it.
func TestWriteInboxShowsTheNameOfTheProject(t *testing.T) {
	box := inbox.Inbox{
		Queued: []store.OpenTicket{{ID: 1, Project: "/one/two/three/the-name", Title: "the title"}},
	}
	got := render(t, box)
	if want := "  1 the-name  the title"; got[5] != want {
		t.Errorf("the row is %q, want %q", got[5], want)
	}
}

// The words of the flags are in no row of any group. They take up to 240
// characters, and one of those wraps a row three times.
func TestWriteInboxNeverShowsTheWordsOfTheFlags(t *testing.T) {
	box := inbox.Inbox{
		Ready:   []store.OpenTicket{{ID: 4, Project: "/projects/one", Title: "a title"}},
		Running: []store.OpenTicket{{ID: 9, Project: "/projects/one", Title: "a short one"}},
		Queued: []store.OpenTicket{
			{ID: 11, Project: "/projects/one", Title: "a much longer title"},
			{ID: 12, Project: "/projects/one", Title: "short"},
		},
	}

	for _, line := range render(t, box) {
		if strings.HasSuffix(line, " ") {
			t.Errorf("the row ends with a space: %q", line)
		}
	}
}

// An inbox with no ticket takes one line, and not three headings with none
// below each of them. The line says what makes a ticket, because a person who
// has no ticket is a person who has not made one yet.
func TestWriteInboxWithNoTicketAtAll(t *testing.T) {
	got := render(t, inbox.Inbox{})
	if len(got) != 1 {
		t.Fatalf("the inbox takes %d lines, want 1:\n%s", len(got), strings.Join(got, "\n"))
	}
	if !strings.Contains(got[0], "dg ticket") {
		t.Errorf("the line does not say what makes a ticket: %q", got[0])
	}
	for _, heading := range []string{"READY", "RUNNING", "QUEUED"} {
		if strings.Contains(got[0], heading) {
			t.Errorf("the line holds the heading %q: %q", heading, got[0])
		}
	}
}

// One group with a ticket keeps each heading, because the person can see where
// the other groups are.
func TestWriteInboxWithOneGroupKeepsTheHeadings(t *testing.T) {
	box := inbox.Inbox{Queued: []store.OpenTicket{{ID: 1, Project: "/projects/one", Title: "a title"}}}

	got := render(t, box)
	for _, heading := range []string{"READY", "RUNNING", "QUEUED"} {
		if !slices.Contains(got, heading) {
			t.Errorf("the inbox does not hold the heading %q:\n%s", heading, strings.Join(got, "\n"))
		}
	}
}
