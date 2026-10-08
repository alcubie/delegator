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

// testDone supplies the rendering window; doneGroup is its expected heading.
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

// rows selects a section by heading so added groups do not invalidate line-
// number assumptions.
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

// Only active runs show elapsed time; ready tickets must not keep counting.
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

// FAILED appears between RUNNING and QUEUED without row notes; dg show
// provides failure times.
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

// Hide the empty FAILED section so the heading indicates an actual failure.
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

// Show unfinished blockers beside queued tickets, omitting the note once all
// prerequisites are done.
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

// Align blocker notes and elapsed durations at the same right edge.
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

// A wide PTY verifies that both note types follow terminal width.
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

// Piped watch output uses COLUMNS. Invalid or out-of-range values must
// preserve the default width.
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

// Do not show blockers on ready work, which already awaits review.
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

// Truncate titles to preserve note alignment, indicating truncation with an
// ellipsis.
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

// Multibyte characters must be counted as runes, not bytes, when aligning
// rows.
func TestWriteInboxKeepsEachRowOfARunAtOneWidth(t *testing.T) {
	titles := []string{
		"a",
		"a title of a length that reaches no column",
		"a title that is long enough to reach the duration and to go past it",
		"Mové to a néw versión of Go — the ölder one is out",
		"Mové to a néw versión of Go — the ölder one is out of maintenance now",
	}
	// Project names also contribute rune width.
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

// A 43-rune, 49-byte title fits a 44-rune column without truncation.
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

// Legacy running tickets without a run record have no elapsed time to show.
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

// An empty inbox should give ticket-creation guidance instead of empty
// groups.
func TestWriteInboxWithNoTicketAtAll(t *testing.T) {
	got := render(t, inbox.Inbox{QueueRunning: true})
	if len(got) != 2 {
		t.Fatalf("the inbox takes %d lines, want the status and one more:\n%s", len(got), strings.Join(got, "\n"))
	}
	if !strings.Contains(got[1], "dg ticket create") {
		t.Errorf("the line does not say what makes a ticket: %q", got[1])
	}
	for _, heading := range []string{"DONE", "READY", "RUNNING", "FAILED", "QUEUED"} {
		if strings.Contains(got[1], heading) {
			t.Errorf("the line holds the heading %q: %q", heading, got[1])
		}
	}
}

// Always show queue state, including when the inbox is empty.
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

// Exercise the database-to-renderer path for elapsed time, allowing a few
// seconds for the test to run.
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

// DONE appears first and shows no running duration.
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

// DONE-only inboxes still contain work and must not show the empty-inbox
// message.
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

// DONE shares column widths with open groups.
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

// Use a nondefault window to detect hard-coded heading text.
func TestWriteInboxSaysTheWindowOfDone(t *testing.T) {
	box := inbox.Inbox{Queued: []store.OpenTicket{{ID: 1, Project: "/projects/one", Title: "a title"}}}

	var out bytes.Buffer
	writeInbox(&out, box, colourAuto, testNow, 6*time.Hour)

	got := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if want := "DONE (last 6h)"; !slices.Contains(got, want) {
		t.Errorf("the inbox does not hold the heading %q:\n%s", want, strings.Join(got, "\n"))
	}
}

func TestStatusLineIsPlainOffATerminal(t *testing.T) {
	for _, box := range []inbox.Inbox{{QueueRunning: true}, {}} {
		var buf bytes.Buffer
		if got := statusLine(&buf, box, colourAuto); strings.Contains(got, "\x1b") {
			t.Errorf("the status line holds an escape code off a terminal: %q", got)
		}
	}
}

// Check status colors through the rendering entry point.
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

// Exercise acceptance through storage, inbox grouping, and terminal
// rendering.
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

// Exercise unreported-run failure through storage, grouping, and rendering.
func TestRunShowsAFailedTicketInFailed(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	s := testfix.OpenStore(t, dataDir)
	id := queuedIn(t, s, repo, "ticket title")
	if _, err := s.Claim(id, fmt.Sprintf("delegator/%d-a-title", id), testAgentID); err != nil {
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

// A zero DONE window excludes even newly accepted tickets.
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

// A six-hour setting proves the heading reads the database rather than the
// 24-hour default.
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
