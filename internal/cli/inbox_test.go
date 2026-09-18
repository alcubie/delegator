package cli

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/creack/pty"

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

// FAILED holds the ticket whose run stopped without a report, and its row
// carries no note: the time of the failure is on dg show, and the heading
// alone is what the inbox must give. The group is between RUNNING and QUEUED,
// where it has always been, so a person who has seen it before finds it in the
// place they remember.
func TestWriteInboxShowsAFailedTicket(t *testing.T) {
	box := inbox.Inbox{
		QueueRunning: true,
		Failed: []store.OpenTicket{{
			ID: 10, Project: "/projects/data-loader", Title: "Fix the query that broke the build",
			Status: store.Failed, Changed: testNow.Add(-2 * time.Hour),
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
		"FAILED",
		" 10 data-loader  Fix the query that broke the build",
		"QUEUED",
		"  none",
	})
}

// A run that went as it should leaves FAILED empty, and then the inbox has no
// FAILED in it at all: a heading with none below it on nearly every run is a
// line that says nothing, and a person reads past it until the day it matters.
// The heading is there only when something is under it, so it reads as news.
func TestWriteInboxLeavesFailedOutWhenItIsEmpty(t *testing.T) {
	box := inbox.Inbox{
		QueueRunning: true,
		Queued: []store.OpenTicket{{
			ID: 9, Project: "/projects/one", Title: "a title", Status: store.Queued,
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
		"  9 one  a title",
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
		"  9 web-api  Move to a new version of Go                      #4 #7",
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
		" 14 web-api  Add a limit on the rate                             #9",
	})
}

// The notes at the right of rows belong to the edge of the terminal rather
// than to a fixed-width table. A wide pseudo-terminal stands in for a resized
// terminal, and both kinds of noted row take every column it offers.
func TestWriteInboxEndsNotesAtTheTerminalEdge(t *testing.T) {
	// A real terminal wins over a COLUMNS inherited from an outer terminal.
	t.Setenv("COLUMNS", "80")
	output, terminal, err := pty.Open()
	if err != nil {
		t.Skipf("no pseudo-terminal to test with: %v", err)
	}
	defer output.Close()
	defer terminal.Close()

	const columns = 100
	if err := pty.Setsize(terminal, &pty.Winsize{Rows: 24, Cols: columns}); err != nil {
		t.Skipf("cannot size the pseudo-terminal: %v", err)
	}
	box := inbox.Inbox{
		Running: []store.OpenTicket{{
			ID: 9, Project: "/projects/web-api", Title: "Move to a new version of Go",
			Status: store.Running, Started: testNow.Add(-time.Minute),
		}},
		Queued: []store.OpenTicket{{
			ID: 14, Project: "/projects/web-api", Title: "Add a limit on the rate",
			Status: store.Queued, DependsOn: []int64{4, 7},
		}},
	}
	writeInbox(terminal, box, colourNever, testNow, testDone)

	reader := bufio.NewReader(output)
	found := make(map[string]bool)
	for range 9 {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		line = strings.TrimRight(line, "\r\n")
		for _, note := range []string{"00:01:00", "#4 #7"} {
			if !strings.HasSuffix(line, note) {
				continue
			}
			found[note] = true
			if got := utf8.RuneCountInString(line); got != columns {
				t.Errorf("the row is %d columns wide, want terminal width %d: %q", got, columns, line)
			}
		}
	}
	for _, note := range []string{"00:01:00", "#4 #7"} {
		if !found[note] {
			t.Errorf("the terminal output holds no row ending in %q", note)
		}
	}
}

// watch gives dg a pipe for stdout, so the ioctl cannot see the terminal, but
// it exports the width as COLUMNS. Values outside the positive 16-bit range of
// a terminal size are not widths and leave redirected output at its stable
// default instead.
func TestOutputWidthReadsColumnsForWatch(t *testing.T) {
	output, pipe, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	defer pipe.Close()

	for name, test := range map[string]struct {
		columns string
		want    int
	}{
		"watch width":  {"100", 100},
		"unset":        {"", defaultRowWidth},
		"not a number": {"wide", defaultRowWidth},
		"zero":         {"0", defaultRowWidth},
		"negative":     {"-1", defaultRowWidth},
		"too large":    {"65536", defaultRowWidth},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("COLUMNS", test.columns)
			if got := outputWidth(pipe); got != test.want {
				t.Errorf("outputWidth with COLUMNS=%q = %d, want %d", test.columns, got, test.want)
			}
		})
	}
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
			if got := utf8.RuneCountInString(row); got != defaultRowWidth {
				t.Errorf("the row is %d characters wide, want %d: %q", got, defaultRowWidth, row)
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
	for _, heading := range []string{"DONE", "READY", "RUNNING", "FAILED", "QUEUED"} {
		if strings.Contains(got[1], heading) {
			t.Errorf("the line holds the heading %q: %q", heading, got[1])
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

// The whole path: a run that stops without a report puts its ticket in
// FAILED, and dg shows it there. The store, the inbox and the text of the
// group each have their own test, and none of them says that the three are
// connected.
func TestRunShowsAFailedTicketInFailed(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	s := testfix.OpenStore(t, dataDir)
	id := queuedIn(t, s, repo, "ticket title")
	if _, err := s.Claim(id, fmt.Sprintf("delegator/%d-a-title", id)); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeStatus(id, store.Failed); err != nil {
		t.Fatal(err)
	}

	out, err := runIn(t, dataDir, repo)
	if err != nil {
		t.Fatal(err)
	}
	got := rows(t, strings.Split(strings.TrimRight(out, "\n"), "\n"), "FAILED")
	want := regexp.MustCompile(`^ +` + strconv.FormatInt(id, 10) + ` \S+  ticket title$`)
	if len(got) != 1 || !want.MatchString(got[0]) {
		t.Errorf("FAILED holds %v, want the row of ticket %d:\n%s", got, id, out)
	}
}

// done_hours in the settings row says how far back DONE reaches. At 0 the group
// is empty, and a ticket accepted a moment ago is already out of the window.
func TestRunTakesTheWindowOfDoneFromTheConfig(t *testing.T) {
	dataDir := t.TempDir()
	s, id, repo := readyTicket(t, dataDir)
	if err := s.SetSetting("done_hours", "0"); err != nil {
		t.Fatal(err)
	}
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
// only have come from the database.
func TestRunSaysTheWindowOfDoneFromTheConfig(t *testing.T) {
	dataDir := t.TempDir()
	s, _, repo := readyTicket(t, dataDir)
	if err := s.SetSetting("done_hours", "6"); err != nil {
		t.Fatal(err)
	}

	out, err := runIn(t, dataDir, repo)
	if err != nil {
		t.Fatal(err)
	}
	if want := "DONE (last 6h)"; !strings.Contains(out, want) {
		t.Errorf("the inbox does not hold the heading %q:\n%s", want, out)
	}
}
