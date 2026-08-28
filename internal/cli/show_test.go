package cli

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/store"
)

func TestWrapBreaksAtASpace(t *testing.T) {
	got := wrap("the quick brown fox jumps over the lazy dog", 20)
	want := []string{"the quick brown fox", "jumps over the lazy", "dog"}
	if !slices.Equal(got, want) {
		t.Errorf("wrap = %q, want %q", got, want)
	}
}

func TestWrapWithAWordLongerThanTheWidth(t *testing.T) {
	// A word with no space in it cannot break, so it takes its own line and
	// goes past the width. A path of a worktree is such a word.
	got := wrap("a supercalifragilistic word", 10)
	want := []string{"a", "supercalifragilistic", "word"}
	if !slices.Equal(got, want) {
		t.Errorf("wrap = %q, want %q", got, want)
	}
}

func TestWrapWithNoText(t *testing.T) {
	if got := wrap("", 20); len(got) != 0 {
		t.Errorf("wrap = %q, want nothing", got)
	}
}

func TestWrapKeepsEachEmptyLineOfTheProse(t *testing.T) {
	got := wrap("one\n\ntwo", 20)
	want := []string{"one", "", "two"}
	if !slices.Equal(got, want) {
		t.Errorf("wrap = %q, want %q", got, want)
	}
}

func TestAgo(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		when string
		want string
	}{
		{"2026-08-28T11:59:30Z", "just now"},
		{"2026-08-28T11:58:00Z", "2m ago"},
		{"2026-08-28T10:00:00Z", "2h ago"},
		{"2026-08-26T12:00:00Z", "2d ago"},
		{"", ""},
		{"not a time", ""},
	}
	for _, test := range tests {
		if got := ago(test.when, now); got != test.want {
			t.Errorf("ago(%q) = %q, want %q", test.when, got, test.want)
		}
	}
}

func TestAgoInTheFuture(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	if got := ago("2026-08-28T13:00:00Z", now); got != "just now" {
		t.Errorf("ago = %q, want %q", got, "just now")
	}
}

// A path below the home of the person takes a tilde in place of it, which is
// what a shell writes and what a person reads.
func TestTilde(t *testing.T) {
	tests := []struct {
		path, home, want string
	}{
		{"/home/person/.local/share/delegator/tickets/4.md", "/home/person",
			"~/.local/share/delegator/tickets/4.md"},
		{"/home/person", "/home/person", "~"},
		{"/projects/one", "/home/person", "/projects/one"},
		// a directory whose name starts with the home is not below it
		{"/home/person2/work", "/home/person", "/home/person2/work"},
		// no home means no change
		{"/home/person/work", "", "/home/person/work"},
	}
	for _, test := range tests {
		if got := tilde(test.path, test.home); got != test.want {
			t.Errorf("tilde(%q, %q) = %q, want %q", test.path, test.home, got, test.want)
		}
	}
}

// showTicketLines writes one ticket and returns each line of it.
func showTicketLines(t *testing.T, ticket store.Ticket, prose string) []string {
	t.Helper()
	var out bytes.Buffer
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	writeTicket(&out, "/data", ticket, prose, now)
	return strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
}

func TestWriteTicketHoldsEachPart(t *testing.T) {
	ticket := store.Ticket{
		ID:        4,
		Project:   "/projects/web-api",
		Title:     "Remove the staging app",
		Status:    store.Ready,
		Branch:    "delegator/4-remove-the-staging-app",
		Session:   "e55e382e-2c88-4de7-a31d-ab8763a0fb5a",
		Result:    "The staging app is removed.",
		Flags:     "the token is not valid",
		Completed: "2026-08-28T10:00:00Z",
	}
	out := strings.Join(showTicketLines(t, ticket, "Remove the staging app and the volume."), "\n")

	for _, want := range []string{
		"#4", "Remove the staging app", "ready", "2h ago",
		"flags", "the token is not valid",
		"result", "The staging app is removed.",
		"ticket", "/data/tickets/4.md",
		"worktree", "/data/worktrees/4",
		"project", "/projects/web-api",
		"branch", "delegator/4-remove-the-staging-app",
		"session", "e55e382e-2c88-4de7-a31d-ab8763a0fb5a",
		"Remove the staging app and the volume.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the ticket does not hold %q:\n%s", want, out)
		}
	}
}

// A ticket that had no run holds no result and no flags, and the two lines say
// none rather than nothing, so the person sees that no run wrote them.
func TestWriteTicketWithNoRun(t *testing.T) {
	ticket := store.Ticket{ID: 3, Project: "/projects/one", Title: "a title", Status: store.Queued}
	out := strings.Join(showTicketLines(t, ticket, ""), "\n")

	if !strings.Contains(out, "flags     none") {
		t.Errorf("the ticket does not say that it has no flags:\n%s", out)
	}
	if !strings.Contains(out, "result    none") {
		t.Errorf("the ticket does not say that it has no result:\n%s", out)
	}
	// no run means no branch and no session, so those lines are not there
	for _, gone := range []string{"branch", "session"} {
		if strings.Contains(out, gone) {
			t.Errorf("the ticket holds %q, and no run wrote one:\n%s", gone, out)
		}
	}
}

// The flags take 240 characters, so the text wraps and each line below the
// first is under the first.
func TestWriteTicketWrapsTheFlags(t *testing.T) {
	long := strings.Repeat("a word ", 40)
	ticket := store.Ticket{ID: 4, Project: "/p/one", Title: "a title", Status: store.Ready, Flags: long}

	var body []string
	for _, line := range showTicketLines(t, ticket, "") {
		if strings.Contains(line, "a word") {
			body = append(body, line)
		}
	}
	if len(body) < 2 {
		t.Fatalf("the flags take %d lines, want more than one", len(body))
	}
	first := strings.Index(body[0], "a word")
	for i, line := range body[1:] {
		if got := strings.Index(line, "a word"); got != first {
			t.Errorf("line %d starts at column %d, want %d", i+1, got, first)
		}
	}
}

// dg show reads the fields from the database and the prose from the file.
func TestRunShowReadsTheRowAndTheFile(t *testing.T) {
	dataDir := t.TempDir()
	repo := gitRepo(t)
	const title = "Remove the staging app"
	const body = "Remove the app, the volume and the records of the DNS."
	if _, err := runIn(t, dataDir, repo, "ticket", title, body); err != nil {
		t.Fatal(err)
	}

	out, err := runIn(t, dataDir, repo, "show", "1")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"#1", title, "queued", body, "ticket", "worktree"} {
		if !strings.Contains(out, want) {
			t.Errorf("dg show does not hold %q:\n%s", want, out)
		}
	}
}

func TestRunShowWithATicketThatIsNotThere(t *testing.T) {
	out, err := runIn(t, t.TempDir(), gitRepo(t), "show", "9999")
	if !errors.Is(err, store.ErrNoTicket) {
		t.Fatalf("err = %v, want ErrNoTicket", err)
	}
	if out != "" {
		t.Errorf("dg show wrote %q, want nothing", out)
	}
}

func TestRunShowWithAnIDThatIsNotANumber(t *testing.T) {
	_, err := runIn(t, t.TempDir(), gitRepo(t), "show", "banana")
	if err == nil {
		t.Fatal("dg show took an id that is not a number")
	}
	if !strings.Contains(err.Error(), "banana") {
		t.Errorf("err = %v, and it does not name what the person wrote", err)
	}
}

func TestRunShowWithNoID(t *testing.T) {
	if _, err := runIn(t, t.TempDir(), gitRepo(t), "show"); err == nil {
		t.Fatal("dg show took no id")
	}
}

// The person owns the prose, and it is markdown that the person wrote with the
// line breaks that they chose. dg show writes it as it is: a re-wrap breaks a
// list, and it makes each long line into one long line and one short one.
func TestWriteTicketKeepsTheProseAsItIs(t *testing.T) {
	prose := "A line that is quite long and holds more than the width of the wrap for the flags.\n" +
		"\n" +
		"1. The first item of a list that is also long enough to go past the width.\n" +
		"2. The second item.\n"
	ticket := store.Ticket{ID: 4, Project: "/p/one", Title: "a title", Status: store.Queued}

	got := showTicketLines(t, ticket, prose)
	for _, want := range []string{
		"  A line that is quite long and holds more than the width of the wrap for the flags.",
		"  1. The first item of a list that is also long enough to go past the width.",
		"  2. The second item.",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("the prose does not hold the line %q:\n%s", want, strings.Join(got, "\n"))
		}
	}
	for _, line := range got {
		if strings.HasSuffix(line, " ") {
			t.Errorf("a line ends with a space: %q", line)
		}
	}
}

// The time beside the status is the time that the ticket became ready, and only
// a ready ticket has one. A ticket that waits or runs keeps a completed from an
// earlier run, because dg revise does not take it away, so a time beside those
// two would say when a run stopped and read as when the ticket arrived.
func TestWriteTicketShowsTheTimeForAReadyTicketOnly(t *testing.T) {
	base := store.Ticket{
		ID: 4, Project: "/p/one", Title: "a title",
		Created:   "2026-08-01T09:00:00Z",
		Completed: "2026-08-28T10:00:00Z",
	}
	for _, test := range []struct {
		status store.TicketStatus
		want   bool
	}{
		{store.Ready, true},
		{store.Queued, false},
		{store.Running, false},
		{store.Failed, false},
	} {
		ticket := base
		ticket.Status = test.status
		heading := showTicketLines(t, ticket, "")[0]
		if got := strings.Contains(heading, "ago"); got != test.want {
			t.Errorf("a %s ticket has a time = %v, want %v: %q", test.status, got, test.want, heading)
		}
		if !strings.Contains(heading, string(test.status)) {
			t.Errorf("the heading does not hold the status: %q", heading)
		}
	}
}

// The text of a field stops where the rule stops. The prose is not in this,
// because the person chose its line breaks and dg show writes them as they are.
func TestWriteTicketKeepsEachFieldInsideTheRule(t *testing.T) {
	ticket := store.Ticket{
		ID: 4, Project: "/projects/a-name-of-some-length", Title: "a title", Status: store.Ready,
		Flags:   strings.Repeat("a word ", 40),
		Result:  strings.Repeat("more words ", 20),
		Branch:  "delegator/4-a-title",
		Session: "e55e382e-2c88-4de7-a31d-ab8763a0fb5a",
	}
	for _, line := range showTicketLines(t, ticket, "") {
		if n := len([]rune(line)); n > ruleWidth {
			t.Errorf("the line takes %d columns and the rule takes %d: %q", n, ruleWidth, line)
		}
	}
}
