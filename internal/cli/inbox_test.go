package cli

import (
	"bytes"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/inbox"
	"github.com/alcubie/delegator/internal/store"
)

// inboxNow is the moment that render writes an inbox at. A test of the
// duration of a run puts the start of the run before it.
var inboxNow = time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)

// render writes one inbox at inboxNow and returns each line of it.
func render(t *testing.T, box inbox.Inbox) []string {
	t.Helper()
	var out bytes.Buffer
	writeInbox(&out, box, colourAuto, inboxNow)
	return strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
}

// wantLines compares an inbox with the lines it must hold.
func wantLines(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("the inbox is\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d is %q, want %q", i, got[i], want[i])
		}
	}
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
		QueueRunning: true,
		Queued:       []store.OpenTicket{{ID: 3, Project: "/projects/one", Title: "the title"}},
	}

	wantLines(t, render(t, box), []string{
		statusRunning,
		"READY",
		"  none",
		"RUNNING",
		"  none",
		"QUEUED",
		"  3 one  the title",
	})
}

// The id is right of its column and each project takes the same width, so the
// titles of two groups are below one another.
func TestWriteInboxPutsTheColumnsTogether(t *testing.T) {
	box := inbox.Inbox{
		QueueRunning: true,
		Ready:        []store.OpenTicket{{ID: 4, Project: "/projects/one", Title: "the first title"}},
		Queued:       []store.OpenTicket{{ID: 11, Project: "/projects/a-longer-name", Title: "the third title"}},
	}

	wantLines(t, render(t, box), []string{
		statusRunning,
		"READY",
		"  4 one            the first title",
		"RUNNING",
		"  none",
		"QUEUED",
		" 11 a-longer-name  the third title",
	})
}

// The project of a ticket is a path, and the inbox shows the name at the end of
// it.
func TestWriteInboxShowsTheNameOfTheProject(t *testing.T) {
	box := inbox.Inbox{
		QueueRunning: true,
		Queued:       []store.OpenTicket{{ID: 1, Project: "/one/two/three/the-name", Title: "the title"}},
	}
	got := render(t, box)
	if want := "  1 the-name  the title"; got[6] != want {
		t.Errorf("the row is %q, want %q", got[6], want)
	}
}

// The person watches the inbox with watch -n 1 dg, and the row of the run
// tells them how long it has been going. A ticket in READY holds the start of
// the run that made it ready, and its row shows no duration: that run stopped,
// and a clock that counts up beside it would say that it had not.
func TestWriteInboxShowsTheDurationOfTheRun(t *testing.T) {
	box := inbox.Inbox{
		QueueRunning: true,
		Ready: []store.OpenTicket{{
			ID: 4, Project: "/projects/one", Title: "the first title",
			Status: store.Ready, Started: inboxNow.Add(-3 * time.Hour),
		}},
		Running: []store.OpenTicket{{
			ID: 9, Project: "/projects/one", Title: "the second title",
			Status: store.Running, Started: inboxNow.Add(-(14*time.Minute + 7*time.Second)),
		}},
	}

	wantLines(t, render(t, box), []string{
		statusRunning,
		"READY",
		"  4 one  the first title",
		"RUNNING",
		"  9 one  the second title  00:14:07",
		"QUEUED",
		"  none",
	})
}

// A ticket that a version before the table runs put in running has no row of
// runs, so there is no time to count from and the row ends with the title.
func TestWriteInboxWithARunningTicketThatHasNoRun(t *testing.T) {
	box := inbox.Inbox{
		QueueRunning: true,
		Running: []store.OpenTicket{{
			ID: 9, Project: "/projects/one", Title: "the title", Status: store.Running,
		}},
	}

	wantLines(t, render(t, box), []string{
		statusRunning,
		"READY",
		"  none",
		"RUNNING",
		"  9 one  the title",
		"QUEUED",
		"  none",
	})
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
	got := render(t, inbox.Inbox{QueueRunning: true})
	if len(got) != 2 {
		t.Fatalf("the inbox takes %d lines, want the status and one more:\n%s", len(got), strings.Join(got, "\n"))
	}
	if !strings.Contains(got[1], "dg ticket") {
		t.Errorf("the line does not say what makes a ticket: %q", got[1])
	}
	for _, heading := range []string{"READY", "RUNNING", "QUEUED"} {
		if strings.Contains(got[1], heading) {
			t.Errorf("the line holds the heading %q: %q", heading, got[1])
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

// The first line always says the state of the queue, so a person never has to
// know what the absence of a line means; and it is there for an empty inbox
// too, because a paused queue with nothing in it is still paused.
func TestWriteInboxStartsWithTheStateOfTheQueue(t *testing.T) {
	queued := []store.OpenTicket{{ID: 1, Title: "a title"}}
	for name, c := range map[string]struct {
		box  inbox.Inbox
		want string
	}{
		"running with tickets": {inbox.Inbox{QueueRunning: true, Queued: queued}, statusRunning},
		"running with none":    {inbox.Inbox{QueueRunning: true}, statusRunning},
		"paused with tickets":  {inbox.Inbox{Queued: queued}, statusPaused},
		"paused with none":     {inbox.Inbox{}, statusPaused},
	} {
		var out bytes.Buffer
		writeInbox(&out, c.box, colourAuto, inboxNow)
		if !strings.HasPrefix(out.String(), c.want+"\n") {
			t.Errorf("%s: the inbox does not start with %q:\n%s", name, c.want, out.String())
		}
	}
}

func TestRunShowsTheStateOfTheQueue(t *testing.T) {
	dataDir := t.TempDir()
	out, err := runIn(t, dataDir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, statusRunning) {
		t.Errorf("dg does not start with %q:\n%s", statusRunning, out)
	}

	if _, err := runIn(t, dataDir, t.TempDir(), "pause"); err != nil {
		t.Fatal(err)
	}
	if out, err = runIn(t, dataDir, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, statusPaused) {
		t.Errorf("dg after dg pause does not start with %q:\n%s", statusPaused, out)
	}
}

// A pipe, a file and a test see plain text: the colour is for a person at a
// terminal, and it would be noise in a log or a grep.
func TestStatusLineIsPlainOffATerminal(t *testing.T) {
	for _, box := range []inbox.Inbox{{QueueRunning: true}, {}} {
		var buf bytes.Buffer
		if got := statusLine(&buf, box, colourAuto); strings.Contains(got, "\x1b") {
			t.Errorf("the status line holds an escape code off a terminal: %q", got)
		}
	}
}

// Green is for a queue that will start work and yellow for one that will not,
// and the pairing has to hold through statusLine, which is what a terminal
// actually gets.
func TestStatusLineColoursAtATerminal(t *testing.T) {
	pty, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal to test with: %v", err)
	}
	defer pty.Close()

	for _, c := range []struct {
		box  inbox.Inbox
		want string
	}{
		{inbox.Inbox{QueueRunning: true}, "Status: " + green + "Running" + plain},
		{inbox.Inbox{}, "Status: " + yellow + "Paused" + plain},
	} {
		if got := statusLine(pty, c.box, colourAuto); got != c.want {
			t.Errorf("status line = %q, want %q", got, c.want)
		}
	}
}
