package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/alcubie/delegator/internal/inbox"
	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

// testDone is the window of DONE that render writes, and doneGroup is the
// heading that window makes. A test names the group by the heading, so it
// holds the window as a person reads it and not as the word alone.
const (
	testDone  = 24 * time.Hour
	doneGroup = "DONE (last 24h)"
)

// render writes one inbox at testNow and returns each line of it.
func render(t *testing.T, box inbox.Inbox) []string {
	t.Helper()
	var out bytes.Buffer
	writeInbox(&out, box, colourAuto, testNow, testDone)
	return strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
}

// rows returns the lines below one heading of an inbox, so a test names the
// group it examines rather than a line number that a new group would move.
func rows(t *testing.T, lines []string, heading string) []string {
	t.Helper()
	at := slices.Index(lines, heading)
	if at < 0 {
		t.Fatalf("the inbox holds no heading %q:\n%s", heading, strings.Join(lines, "\n"))
	}
	var out []string
	for _, line := range lines[at+1:] {
		if !strings.HasPrefix(line, " ") {
			break
		}
		out = append(out, line)
	}
	return out
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

func TestWriteInboxHoldsTheFourGroupsInOneOrder(t *testing.T) {
	box := inbox.Inbox{
		Ready:   []store.OpenTicket{{ID: 4, Project: "/projects/one", Title: "the first title"}},
		Running: []store.OpenTicket{{ID: 9, Project: "/projects/one", Title: "the second title"}},
		Queued:  []store.OpenTicket{{ID: 11, Project: "/projects/a-longer-name", Title: "the third title"}},
	}

	var headings []int
	lines := render(t, box)
	for i, line := range lines {
		if line == doneGroup || line == "READY" || line == "RUNNING" || line == "QUEUED" {
			headings = append(headings, i)
		}
	}
	if len(headings) != 4 {
		t.Fatalf("the inbox holds %d headings, want 4:\n%s", len(headings), strings.Join(lines, "\n"))
	}
	for i, want := range []string{doneGroup, "READY", "RUNNING", "QUEUED"} {
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
		doneGroup,
		"  none",
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
		doneGroup,
		"  none",
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
	got := rows(t, render(t, box), "QUEUED")[0]
	if want := "  1 the-name  the title"; got != want {
		t.Errorf("the row is %q, want %q", got, want)
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
		doneGroup,
		"  none",
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
		doneGroup,
		"  none",
		"READY",
		"  none",
		"RUNNING",
		"  9 web-api      Move to a new version of Go               00:14:07",
		" 14 data-loader  Add a limit on the rate                   03:42:01",
		"QUEUED",
		"  none",
	})
}

// A queued ticket that depends on a ticket that is not done says so at the
// right of its row. A person who sees a ticket at the top of the queue and no
// run needs to know that the queue is passing it over on purpose. A queued
// ticket whose links are all done depends on nothing, holds no id here, and its
// row ends at its title.
func TestWriteInboxNamesTheTicketsAQueuedTicketDependsOn(t *testing.T) {
	box := inbox.Inbox{
		QueueRunning: true,
		Queued: []store.OpenTicket{{
			ID: 9, Project: "/projects/web-api", Title: "Move to a new version of Go",
			Status: store.Queued, DependsOn: []int64{4, 7},
		}, {
			ID: 14, Project: "/projects/web-api", Title: "Add a limit on the rate",
			Status: store.Queued,
		}},
	}

	wantLines(t, render(t, box), []string{
		statusRunning,
		doneGroup,
		"  none",
		"READY",
		"  none",
		"RUNNING",
		"  none",
		"QUEUED",
		"  9 web-api  Move to a new version of Go            waits for #4 #7",
		" 14 web-api  Add a limit on the rate",
	})
}

// The note of a queued ticket and the duration of a run end at the same
// column, because they answer the same question about two rows: what the
// ticket is doing now. A person reads down one edge of the inbox for it.
func TestWriteInboxPutsTheNoteAndTheDurationInOneColumn(t *testing.T) {
	box := inbox.Inbox{
		QueueRunning: true,
		Running: []store.OpenTicket{{
			ID: 9, Project: "/projects/web-api", Title: "Move to a new version of Go",
			Status: store.Running, Started: testNow.Add(-(14*time.Minute + 7*time.Second)),
		}},
		Queued: []store.OpenTicket{{
			ID: 14, Project: "/projects/web-api", Title: "Add a limit on the rate",
			Status: store.Queued, DependsOn: []int64{9},
		}},
	}

	wantLines(t, render(t, box), []string{
		statusRunning,
		doneGroup,
		"  none",
		"READY",
		"  none",
		"RUNNING",
		"  9 web-api  Move to a new version of Go                   00:14:07",
		"QUEUED",
		" 14 web-api  Add a limit on the rate                   waits for #9",
	})
}

// A ticket of READY can hold a link to a ticket that is not accepted, and its
// row says nothing about it: READY waits for the person and not for the queue,
// so the note would name a rule that does not hold there.
func TestWriteInboxLeavesTheLinkOffAReadyRow(t *testing.T) {
	box := inbox.Inbox{
		QueueRunning: true,
		Ready: []store.OpenTicket{{
			ID: 9, Project: "/projects/web-api", Title: "Move to a new version of Go",
			Status: store.Ready, DependsOn: []int64{4},
		}},
	}

	wantLines(t, render(t, box), []string{
		statusRunning,
		doneGroup,
		"  none",
		"READY",
		"  9 web-api  Move to a new version of Go",
		"RUNNING",
		"  none",
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
		doneGroup,
		"  none",
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

			row := rows(t, render(t, box), "RUNNING")[0]
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

	row := rows(t, render(t, box), "RUNNING")[0]
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
		doneGroup,
		"  none",
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
	for _, heading := range []string{"DONE", "READY", "RUNNING", "QUEUED"} {
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
	for _, heading := range []string{doneGroup, "READY", "RUNNING", "QUEUED"} {
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
		writeInbox(&out, c.box, colourAuto, testNow, testDone)
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

// DONE is at the top of the inbox: it is the group the person reads and
// leaves, and what is left to do is below it, where the eyes stop. A row of a
// closed ticket carries no duration, because its run stopped.
func TestWriteInboxPutsDoneAtTheTop(t *testing.T) {
	box := inbox.Inbox{
		QueueRunning: true,
		Done: []store.OpenTicket{{
			ID: 2, Project: "/projects/one", Title: "the closed title",
			Status: store.Done, Started: testNow.Add(-2 * time.Hour),
		}},
		Ready: []store.OpenTicket{{ID: 4, Project: "/projects/one", Title: "the ready title"}},
	}

	wantLines(t, render(t, box), []string{
		statusRunning,
		doneGroup,
		"  2 one  the closed title",
		"READY",
		"  4 one  the ready title",
		"RUNNING",
		"  none",
		"QUEUED",
		"  none",
	})
}

// A person whose only tickets are in DONE finished work today, and the line
// that says how to make a ticket would take that away.
func TestWriteInboxWithOnlyDoneTicketsShowsThem(t *testing.T) {
	box := inbox.Inbox{
		QueueRunning: true,
		Done: []store.OpenTicket{{
			ID: 2, Project: "/projects/one", Title: "the closed title", Status: store.Done,
		}},
	}

	got := render(t, box)
	if slices.Contains(got, emptyInbox) {
		t.Fatalf("the inbox says it holds no ticket:\n%s", strings.Join(got, "\n"))
	}
	if want := []string{"  2 one  the closed title"}; !slices.Equal(rows(t, got, doneGroup), want) {
		t.Errorf("DONE holds %v, want %v", rows(t, got, doneGroup), want)
	}
}

// The columns take DONE in with the other groups, so the title of a closed
// ticket is below the title of an open one.
func TestWriteInboxPutsDoneInTheSameColumns(t *testing.T) {
	box := inbox.Inbox{
		QueueRunning: true,
		Done: []store.OpenTicket{{
			ID: 111, Project: "/projects/a-longer-name", Title: "the closed title",
			Status: store.Done,
		}},
		Queued: []store.OpenTicket{{ID: 4, Project: "/projects/one", Title: "the queued title"}},
	}

	wantLines(t, render(t, box), []string{
		statusRunning,
		doneGroup,
		" 111 a-longer-name  the closed title",
		"READY",
		"  none",
		"RUNNING",
		"  none",
		"QUEUED",
		"   4 one            the queued title",
	})
}

// The heading of DONE says how far back the group reaches. The window is not
// the one that render writes, so the heading cannot be a fixed text that holds
// the number by chance.
func TestWriteInboxSaysTheWindowOfDone(t *testing.T) {
	box := inbox.Inbox{Queued: []store.OpenTicket{{ID: 1, Project: "/projects/one", Title: "a title"}}}

	var out bytes.Buffer
	writeInbox(&out, box, colourAuto, testNow, 6*time.Hour)

	got := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if want := "DONE (last 6h)"; !slices.Contains(got, want) {
		t.Errorf("the inbox does not hold the heading %q:\n%s", want, strings.Join(got, "\n"))
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

// The whole path: dg accept closes a ticket, and the inbox still shows it in
// DONE. The store, the window from the config and the text of the group each
// have their own test, and none of them says that the three are connected.
func TestRunShowsAnAcceptedTicketInDone(t *testing.T) {
	dataDir := t.TempDir()
	_, id, repo := readyTicket(t, dataDir)
	if _, err := runIn(t, dataDir, repo, "accept", strconv.FormatInt(id, 10)); err != nil {
		t.Fatal(err)
	}

	out, err := runIn(t, dataDir, repo)
	if err != nil {
		t.Fatal(err)
	}
	got := rows(t, strings.Split(strings.TrimRight(out, "\n"), "\n"), doneGroup)
	want := regexp.MustCompile(`^ +` + strconv.FormatInt(id, 10) + ` \S+  ticket title$`)
	if len(got) != 1 || !want.MatchString(got[0]) {
		t.Errorf("DONE holds %v, want the row of ticket %d:\n%s", got, id, out)
	}
}

// done_hours in the config file says how far back DONE reaches. At 0 the group
// is empty, and a ticket accepted a moment ago is already out of the window.
func TestRunTakesTheWindowOfDoneFromTheConfig(t *testing.T) {
	configDir := testfix.XDGConfigDir(t)
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"),
		[]byte("done_hours = 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	dataDir := t.TempDir()
	_, id, repo := readyTicket(t, dataDir)
	if _, err := runIn(t, dataDir, repo, "accept", strconv.FormatInt(id, 10)); err != nil {
		t.Fatal(err)
	}

	out, err := runIn(t, dataDir, repo)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "ticket title") {
		t.Errorf("the inbox holds the accepted ticket with done_hours = 0:\n%s", out)
	}
}

// The window in the heading of DONE is the one the person set. done_hours is 6
// and not the 24 of a person who set nothing, so the number in the heading can
// only have come from the file.
func TestRunSaysTheWindowOfDoneFromTheConfig(t *testing.T) {
	configDir := testfix.XDGConfigDir(t)
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"),
		[]byte("done_hours = 6\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	dataDir := t.TempDir()
	_, _, repo := readyTicket(t, dataDir)

	out, err := runIn(t, dataDir, repo)
	if err != nil {
		t.Fatal(err)
	}
	if want := "DONE (last 6h)"; !strings.Contains(out, want) {
		t.Errorf("the inbox does not hold the heading %q:\n%s", want, out)
	}
}
