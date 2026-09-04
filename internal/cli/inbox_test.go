package cli

import (
	"bytes"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/alcubie/delegator/internal/inbox"
	"github.com/alcubie/delegator/internal/store"
)

// render writes one inbox at testNow and returns each line of it.
func render(t *testing.T, box inbox.Inbox) []string {
	t.Helper()
	var out bytes.Buffer
	writeInbox(&out, box, colourAuto, testNow)
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
			Status: store.Ready, Started: testNow.Add(-3 * time.Hour),
		}},
		Running: []store.OpenTicket{{
			ID: 9, Project: "/projects/one", Title: "the second title",
			Status: store.Running, Started: testNow.Add(-(14*time.Minute + 7*time.Second)),
		}},
	}

	wantLines(t, render(t, box), []string{
		statusRunning,
		"READY",
		"  4 one  the first title",
		"RUNNING",
		"  9 one  the second title                                  00:14:07",
		"QUEUED",
		"  none",
	})
}

// The durations of two runs stand in one column at the right of the inbox, so
// a person reads them against one another rather than against the end of each
// title. Two runs are what a fault leaves behind, and version 1 has one run at
// a time, so the column is what the person sees after that fault.
func TestWriteInboxPutsTheDurationsInOneColumn(t *testing.T) {
	box := inbox.Inbox{
		QueueRunning: true,
		Running: []store.OpenTicket{{
			ID: 9, Project: "/projects/web-api", Title: "Move to a new version of Go",
			Status: store.Running, Started: testNow.Add(-(14*time.Minute + 7*time.Second)),
		}, {
			ID: 14, Project: "/projects/data-loader", Title: "Add a limit on the rate",
			Status: store.Running, Started: testNow.Add(-(3*time.Hour + 42*time.Minute + time.Second)),
		}},
	}

	wantLines(t, render(t, box), []string{
		statusRunning,
		"READY",
		"  none",
		"RUNNING",
		"  9 web-api      Move to a new version of Go               00:14:07",
		" 14 data-loader  Add a limit on the rate                   03:42:01",
		"QUEUED",
		"  none",
	})
}

// A title that would reach the column of the durations is cut, and an ellipsis
// says that it was. The title of a ticket is the one field of a row that has
// no width of its own, so it is the field that gives way.
func TestWriteInboxCutsATitleThatReachesTheDuration(t *testing.T) {
	box := inbox.Inbox{
		QueueRunning: true,
		Running: []store.OpenTicket{{
			ID: 9, Project: "/projects/web-api",
			Title:  "Show the duration of a run on the RUNNING row of the inbox",
			Status: store.Running, Started: testNow.Add(-(14*time.Minute + 7*time.Second)),
		}},
	}

	wantLines(t, render(t, box), []string{
		statusRunning,
		"READY",
		"  none",
		"RUNNING",
		"  9 web-api  Show the duration of a run on the RUNNING r…  00:14:07",
		"QUEUED",
		"  none",
	})
}

// Each row of a run is as wide as the rule of dg show, whatever its title, so
// the durations line up and nothing runs past the width the two commands
// share. A title of a person can hold a character that takes more than one
// byte, and the width of a row is a count of characters: a row measured in
// bytes comes up short by one column for each byte past the first.
func TestWriteInboxKeepsEachRowOfARunAtOneWidth(t *testing.T) {
	titles := []string{
		"a",
		"a title of a length that reaches no column",
		"a title that is long enough to reach the duration and to go past it",
		"Mové to a néw versión of Go — the ölder one is out",
		"Mové to a néw versión of Go — the ölder one is out of maintenance now",
	}
	// The name of a project is on the row before the title, so a name that
	// holds such a character moves the title by as much.
	projects := []string{"/projects/web-api", "/projects/wéb-àpi"}

	for _, project := range projects {
		for _, title := range titles {
			box := inbox.Inbox{Running: []store.OpenTicket{{
				ID: 9, Project: project, Title: title,
				Status: store.Running, Started: testNow.Add(-time.Minute),
			}}}

			row := render(t, box)[4]
			if got := utf8.RuneCountInString(row); got != rowWidth {
				t.Errorf("the row is %d characters wide, want %d: %q", got, rowWidth, row)
			}
		}
	}
}

// A title that fits in the row keeps every character of itself. The count is
// in characters: a title of 43 characters and 49 bytes fits a column of 44,
// and a row that counted its bytes would cut a title that had room.
func TestWriteInboxKeepsATitleThatFitsInCharacters(t *testing.T) {
	title := "Mové to a néw versión — the ölder Go is out"
	box := inbox.Inbox{Running: []store.OpenTicket{{
		ID: 9, Project: "/projects/web-api", Title: title,
		Status: store.Running, Started: testNow.Add(-time.Minute),
	}}}

	row := render(t, box)[4]
	if !strings.Contains(row, title) {
		t.Errorf("the row does not hold the whole title: %q", row)
	}
	if strings.Contains(row, ellipsis) {
		t.Errorf("the row cut a title that fits: %q", row)
	}
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
		writeInbox(&out, c.box, colourAuto, testNow)
		if !strings.HasPrefix(out.String(), c.want+"\n") {
			t.Errorf("%s: the inbox does not start with %q:\n%s", name, c.want, out.String())
		}
	}
}

// The duration on the row comes from the database and from the clock, and
// writeInbox alone does not say that either one is connected. A claim writes
// runs.started_at a moment before dg reads it, so the row holds a duration of
// zero seconds or of one or two more.
func TestRunShowsTheDurationOfTheRun(t *testing.T) {
	dataDir := t.TempDir()
	_, id, repo, _ := runningTicket(t, dataDir)

	out, err := runIn(t, dataDir, repo)
	if err != nil {
		t.Fatal(err)
	}
	row := regexp.MustCompile(`(?m)^ +` + strconv.FormatInt(id, 10) + ` \S+  ticket title +00:00:0\d$`)
	if !row.MatchString(out) {
		t.Errorf("no row of a run with a duration in the inbox:\n%s", out)
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
