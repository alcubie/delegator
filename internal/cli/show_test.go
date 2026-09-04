package cli

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
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
		Project:   store.Project{Path: "/projects/web-api"},
		Title:     "Remove the staging app",
		Status:    store.Ready,
		Branch:    "delegator/4-remove-the-staging-app",
		Session:   "e55e382e-2c88-4de7-a31d-ab8763a0fb5a",
		Completed: "2026-08-28T10:00:00Z",
	}
	out := strings.Join(showTicketLines(t, ticket, "Remove the staging app and the volume."), "\n")

	for _, want := range []string{
		"#4", "Remove the staging app", "ready", "2h ago",
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

// dg show reads the fields from the database and the prose from the file.
func TestRunShowReadsTheRowAndTheFile(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
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
	out, err := runIn(t, t.TempDir(), testfix.Repo(t, repoBranch), "show", "9999")
	if !errors.Is(err, store.ErrNoTicket) {
		t.Fatalf("err = %v, want ErrNoTicket", err)
	}
	if out != "" {
		t.Errorf("dg show wrote %q, want nothing", out)
	}
}

func TestRunShowWithAnIDThatIsNotANumber(t *testing.T) {
	_, err := runIn(t, t.TempDir(), testfix.Repo(t, repoBranch), "show", "banana")
	if err == nil {
		t.Fatal("dg show took an id that is not a number")
	}
	if !strings.Contains(err.Error(), "banana") {
		t.Errorf("err = %v, and it does not name what the person wrote", err)
	}
}

func TestRunShowWithNoID(t *testing.T) {
	if _, err := runIn(t, t.TempDir(), testfix.Repo(t, repoBranch), "show"); err == nil {
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
	ticket := store.Ticket{ID: 4, Project: store.Project{Path: "/p/one"}, Title: "a title", Status: store.Queued}

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
		ID: 4, Project: store.Project{Path: "/p/one"}, Title: "a title",
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
		ID: 4, Project: store.Project{Path: "/projects/a-name-of-some-length"}, Title: "a title", Status: store.Ready,
		Branch:  "delegator/4-a-title",
		Session: "e55e382e-2c88-4de7-a31d-ab8763a0fb5a",
	}
	for _, line := range showTicketLines(t, ticket, "") {
		if n := len([]rune(line)); n > ruleWidth {
			t.Errorf("the line takes %d columns and the rule takes %d: %q", n, ruleWidth, line)
		}
	}
}

// showCommit returns the commit row of dg show for a ticket whose commit is the
// head of repo.
func showCommit(t *testing.T, repo, hash string) string {
	t.Helper()
	var out bytes.Buffer
	writeTicket(&out, t.TempDir(), store.Ticket{
		ID:      4,
		Project: store.Project{Path: repo},
		Title:   "a title",
		Status:  store.Ready,
		Commit:  hash,
	}, "", time.Now())

	for line := range strings.SplitSeq(out.String(), "\n") {
		if strings.Contains(line, "commit") {
			return line
		}
	}
	return ""
}

func TestWriteTicketGivesTheShortHashAndTheSubject(t *testing.T) {
	repo := testfix.Repo(t, repoBranch)
	testfix.CommitIn(t, repo, "Remove the staging app and its DNS records")
	hash := testfix.GitOut(t, repo, "rev-parse", "HEAD")

	line := showCommit(t, repo, hash)
	want := hash[:7] + " Remove the staging app and its DNS records"
	if !strings.Contains(line, want) {
		t.Errorf("the commit row is %q, and it does not hold %q", line, want)
	}
	if strings.Contains(line, hash) {
		t.Errorf("the commit row is %q, and it holds the whole hash", line)
	}
}

// The subject comes from git at each call, so a message written again is right.
func TestWriteTicketReadsTheSubjectFromGitEachTime(t *testing.T) {
	repo := testfix.Repo(t, repoBranch)
	testfix.CommitIn(t, repo, "the first message")
	testfix.GitIn(t, repo, "-c", "user.email=test@example.com", "-c", "user.name=Test",
		"commit", "--amend", "--allow-empty", "-q", "-m", "the message written again")
	hash := testfix.GitOut(t, repo, "rev-parse", "HEAD")

	if line := showCommit(t, repo, hash); !strings.Contains(line, "the message written again") {
		t.Errorf("the commit row is %q, want the new message", line)
	}
}

// Only the repository changed, so the ticket is still correct and the hash is
// what a person needs to go looking.
func TestWriteTicketWithACommitThatGitDoesNotKnow(t *testing.T) {
	const gone = "0123456789abcdef0123456789abcdef01234567"
	line := showCommit(t, testfix.Repo(t, repoBranch), gone)
	if !strings.Contains(line, gone[:7]) {
		t.Errorf("the commit row is %q, and it does not hold the hash", line)
	}
}

func TestWriteTicketWithNoCommitGivesNoRow(t *testing.T) {
	if line := showCommit(t, testfix.Repo(t, repoBranch), ""); line != "" {
		t.Errorf("the commit row is %q, want none", line)
	}
}

// A person opens the conversation of a run while the agent works, with
// claude --resume <session>, so dg show on a running ticket gives the
// session. The agent reports its id and then sleeps, and dg show is read
// while dg run is still waiting on it.
func TestRunShowGivesTheSessionOfARunningTicket(t *testing.T) {
	dataDir := t.TempDir()
	_, id, repo := queuedTicket(t, dataDir)
	useAgent(t, fakeAgent(t, "run printf 'session: s-1\\n'", "run sleep 1", "exit 0"))

	done := make(chan error, 1)
	go func() {
		_, err := runIn(t, dataDir, repo, "run", fmt.Sprint(id))
		done <- err
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		out, err := runIn(t, dataDir, repo, "show", fmt.Sprint(id))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "session") && strings.Contains(out, "s-1") {
			if !strings.Contains(out, "running") {
				t.Errorf("dg show gives the session on a ticket that is not running:\n%s", out)
			}
			break
		}
		select {
		case err := <-done:
			t.Fatalf("dg run returned (err = %v) and dg show had no session while the agent was alive:\n%s", err, out)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("dg show gave no session after 5s:\n%s", out)
		}
		time.Sleep(10 * time.Millisecond)
	}

	select {
	case err := <-done:
		t.Fatalf("dg run returned (err = %v) before dg show gave the session", err)
	default:
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
