package cli

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/run"
	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

// queuedIn creates a queued ticket, registering repo on its first use.
func queuedIn(t *testing.T, s *store.Store, repo, title string) int64 {
	t.Helper()
	projectID, err := s.ProjectID(repo, repoBranch)
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.AddTicket(projectID, title)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// finishIn moves a queued ticket through a run to Ready and returns its
// branch.
func finishIn(t *testing.T, s *store.Store, id int64) string {
	t.Helper()
	branch := fmt.Sprintf("delegator/%d-a-title", id)
	ticket, err := s.Ticket(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := project.Command(ticket.Project.Path, "rev-parse", "--verify", "HEAD").Run(); err != nil {
		testfix.CommitIn(t, ticket.Project.Path, "first")
	}
	testfix.GitIn(t, ticket.Project.Path, "branch", branch)
	if _, err := s.Claim(id, branch, testAgentID); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishTicket(id, ""); err != nil {
		t.Fatal(err)
	}
	return branch
}

// assertReadyOrder verifies the fixture's ready positions in head-first order.
func assertReadyOrder(t *testing.T, s *store.Store, ids ...int64) {
	t.Helper()
	tickets, err := s.OpenTickets()
	if err != nil {
		t.Fatal(err)
	}
	positions := make(map[int64]int, len(tickets))
	for _, ticket := range tickets {
		if ticket.Status == store.Ready {
			positions[ticket.ID] = ticket.Position
		}
	}
	for position, id := range ids {
		if got := positions[id]; got != position+1 {
			t.Errorf("ticket %d ready position = %d, want %d", id, got, position+1)
		}
	}
}

// showJSON reads the show document through dg rpc.
func showJSON(t *testing.T, dataDir, workDir string, args ...string) map[string]any {
	t.Helper()
	return rpcDocument(t, dataDir, workDir, "show", args...)
}

// jsonFields is every key that RPC show result writes, in the order that the
// text form gives the fields.
var jsonFields = []string{
	"id", "title", "status", "project", "ticket", "worktree",
	"branch", "session", "commit", "created", "accepted", "prose",
}

func TestWrapBreaksAtASpace(t *testing.T) {
	got := wrap("the quick brown fox jumps over the lazy dog", 20)
	want := []string{"the quick brown fox", "jumps over the lazy", "dog"}
	if !slices.Equal(got, want) {
		t.Errorf("wrap = %q, want %q", got, want)
	}
}

func TestWrapWithAWordLongerThanTheWidth(t *testing.T) {
	// Unbreakable paths may exceed the wrap width.
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
	tests := []struct {
		when time.Time
		want string
	}{
		{time.Date(2026, 8, 28, 11, 59, 30, 0, time.UTC), "just now"},
		{time.Date(2026, 8, 28, 11, 58, 0, 0, time.UTC), "2m ago"},
		{time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC), "2h ago"},
		{time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC), "2d ago"},
		{time.Time{}, ""},
	}
	for _, test := range tests {
		if got := ago(test.when, testNow); got != test.want {
			t.Errorf("ago(%v) = %q, want %q", test.when, got, test.want)
		}
	}
}

func TestAgoInTheFuture(t *testing.T) {
	if got := ago(testNow.Add(time.Hour), testNow); got != "just now" {
		t.Errorf("ago = %q, want %q", got, "just now")
	}
}

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

// showTicketLines renders at testNow with an existing worktree. A zero
// started value represents a never-run ticket.
func showTicketLines(t *testing.T, ticket store.Ticket, prose string, started time.Time) []string {
	t.Helper()
	var out bytes.Buffer
	writeTicket(&out, shown{
		Ticket:    ticket,
		ProseFile: proseFile("/data", ticket.ID),
		Prose:     prose,
		Worktree:  run.WorktreePath("/data", ticket.ID),
		Started:   started,
	}, testNow)
	return strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
}

// timeOf extracts the timestamp line between heading and rule, or empty when
// absent.
func timeOf(t *testing.T, lines []string) string {
	t.Helper()
	line := strings.TrimSpace(lines[1])
	if strings.HasPrefix(line, "─") {
		return ""
	}
	return line
}

func TestWriteTicketHoldsEachPart(t *testing.T) {
	ticket := store.Ticket{
		ID:      4,
		Project: store.Project{Path: "/projects/web-api"},
		Title:   "Remove the staging app",
		Status:  store.Ready,
		Branch:  "delegator/4-remove-the-staging-app",
		Session: "e55e382e-2c88-4de7-a31d-ab8763a0fb5a",
		Changed: time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC),
	}
	out := strings.Join(showTicketLines(t, ticket, "Remove the staging app and the volume.", time.Time{}), "\n")

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

// Omit empty dependency rows as with other missing fields.
func TestWriteTicketWithNoLinkWritesNoRow(t *testing.T) {
	var out bytes.Buffer
	writeTicket(&out, shown{Ticket: store.Ticket{
		ID:      4,
		Project: store.Project{Path: "/projects/web-api"},
		Title:   "Remove the staging app",
		Status:  store.Queued,
	}, ProseFile: proseFile("/data", 4)}, testNow)

	if strings.Contains(out.String(), "depends") || strings.Contains(out.String(), "blocks") {
		t.Errorf("dg show holds a row of links for a ticket that has none:\n%s", out.String())
	}
}

// A ticket can both wait on earlier work and let later work go. Both relations
// name every linked ticket, and blocks follows depends on in the display.
func TestWriteTicketNamesWhatItBlocks(t *testing.T) {
	var out bytes.Buffer
	writeTicket(&out, shown{Ticket: store.Ticket{
		ID:      4,
		Project: store.Project{Path: "/projects/web-api"},
		Title:   "Remove the staging app",
		Status:  store.Queued,
	}, ProseFile: proseFile("/data", 4), DependsOn: []int64{1, 2}, Blocks: []int64{5, 6}}, testNow)

	text := out.String()
	depends := strings.Index(text, "depends on  #1 #2")
	blocks := strings.Index(text, "blocks      #5 #6")
	if depends < 0 || blocks < 0 || blocks < depends {
		t.Errorf("dg show links = %q, want depends on followed by blocks", text)
	}
}

// Show completed dependencies too, preserving the full relationship record.
func TestRunShowNamesEveryLinkIncludingADoneOne(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	s := testfix.OpenStore(t, dataDir)
	first := queuedIn(t, s, repo, "the work it builds on")
	second := queuedIn(t, s, repo, "the other work")
	finishIn(t, s, first)
	if err := s.ChangeStatus(first, store.Done); err != nil {
		t.Fatal(err)
	}

	out, err := runIn(t, dataDir, repo, "ticket",
		"--after", fmt.Sprint(first), "--after", fmt.Sprint(second), "Remove the last of it", "--no-body")
	if err != nil {
		t.Fatal(err)
	}
	out, err = runIn(t, dataDir, repo, "show", fmt.Sprint(idOf(t, out)))
	if err != nil {
		t.Fatal(err)
	}

	if want := fmt.Sprintf("depends on  #%d #%d", first, second); !strings.Contains(out, want) {
		t.Errorf("dg show does not hold %q:\n%s", want, out)
	}
}

// Exercise stored run timestamps through dg show, allowing a few seconds of
// elapsed test time.
func TestRunShowGivesTheDurationOfTheRun(t *testing.T) {
	dataDir := t.TempDir()
	_, id, repo, _ := runningTicket(t, dataDir)

	out, err := runIn(t, dataDir, repo, "show", strconv.FormatInt(id, 10))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out, "\n")
	if !strings.HasSuffix(lines[0], string(store.Running)) {
		t.Errorf("the heading does not end with the status: %q", lines[0])
	}
	if !regexp.MustCompile(`^ +00:00:0\d$`).MatchString(lines[1]) {
		t.Errorf("the line below the heading holds no duration of the run: %q", lines[1])
	}
}

// Failed tickets show their failure timestamp beside the status.
func TestRunShowOnAFailedTicketSaysFailedAndGivesTheTime(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	s := testfix.OpenStore(t, dataDir)
	id := queuedIn(t, s, repo, "the ticket that failed")
	if _, err := s.Claim(id, fmt.Sprintf("delegator/%d-a-title", id), testAgentID); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeStatus(id, store.Failed); err != nil {
		t.Fatal(err)
	}

	out, err := runIn(t, dataDir, repo, "show", fmt.Sprint(id))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out, "\n")
	if !strings.HasSuffix(lines[0], "failed") {
		t.Errorf("the heading does not end with the status: %q", lines[0])
	}
	if !regexp.MustCompile(`^ +(just now|\d+[mhd] ago)$`).MatchString(lines[1]) {
		t.Errorf("the line below the heading holds no time of the failure: %q", lines[1])
	}
}

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
	for _, want := range []string{"#1", title, "queued", body, "ticket"} {
		if !strings.Contains(out, want) {
			t.Errorf("dg show does not hold %q:\n%s", want, out)
		}
	}
}

func TestRunShowGivesTheAgentOfTheLastRun(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	s := testfix.OpenStore(t, dataDir)
	id := queuedIn(t, s, repo, "Record the agent")
	agentID, err := s.AgentID("claude")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(id, "delegator/1-record-the-agent", agentID); err != nil {
		t.Fatal(err)
	}

	out, err := runIn(t, dataDir, repo, "show", fmt.Sprint(id))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "agent       claude") {
		t.Errorf("dg show does not hold the agent of the run:\n%s", out)
	}
}

func TestRunShowWithNoRunGivesNoAgent(t *testing.T) {
	dataDir := t.TempDir()
	_, id, repo := queuedTicket(t, dataDir)

	out, err := runIn(t, dataDir, repo, "show", fmt.Sprint(id))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "agent") {
		t.Errorf("dg show holds an agent for a ticket with no run:\n%s", out)
	}
}

func TestRunShowGivesTheWorktreeUntilAcceptTakesItAway(t *testing.T) {
	dataDir := t.TempDir()
	_, id, repo := readyTicket(t, dataDir)

	out, err := runIn(t, dataDir, repo, "show", fmt.Sprint(id))
	if err != nil {
		t.Fatal(err)
	}
	if want := run.WorktreePath(dataDir, id); !strings.Contains(out, want) {
		t.Errorf("dg show does not hold the worktree %q:\n%s", want, out)
	}

	if _, err := runIn(t, dataDir, repo, "accept", fmt.Sprint(id)); err != nil {
		t.Fatal(err)
	}
	out, err = runIn(t, dataDir, repo, "show", fmt.Sprint(id))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "worktree") {
		t.Errorf("dg show holds a worktree that dg accept removed:\n%s", out)
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

// Preserve Markdown line breaks, including lists and long lines.
func TestWriteTicketKeepsTheProseAsItIs(t *testing.T) {
	prose := "A line that is quite long and holds more than the width of the wrap for the flags.\n" +
		"\n" +
		"1. The first item of a list that is also long enough to go past the width.\n" +
		"2. The second item.\n"
	ticket := store.Ticket{ID: 4, Project: store.Project{Path: "/p/one"}, Title: "a title", Status: store.Queued}

	got := showTicketLines(t, ticket, prose, time.Time{})
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

// Use the latest transition for status age; running tickets instead use
// elapsed run time.
func TestWriteTicketShowsTheTimeOfTheLastChange(t *testing.T) {
	base := store.Ticket{
		ID: 4, Project: store.Project{Path: "/p/one"}, Title: "a title",
		Changed: time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC),
	}
	for _, test := range []struct {
		status store.TicketStatus
		want   string
	}{
		{store.Ready, "2h ago"},
		{store.Queued, "2h ago"},
		{store.Failed, "2h ago"},
		{store.Done, "2h ago"},
		{store.Cancelled, "2h ago"},
		{store.Running, ""},
	} {
		ticket := base
		ticket.Status = test.status
		lines := showTicketLines(t, ticket, "", time.Time{})
		if got := timeOf(t, lines); got != test.want {
			t.Errorf("a %s ticket has the time %q, want %q", test.status, got, test.want)
		}
		if !strings.Contains(lines[0], string(test.status)) {
			t.Errorf("the heading does not hold the status: %q", lines[0])
		}
	}
}

// Keep the time on a separate line aligned under status, leaving room for
// long titles.
func TestWriteTicketPutsTheTimeBelowTheStatus(t *testing.T) {
	ticket := store.Ticket{
		ID: 4, Project: store.Project{Path: "/p/one"},
		Title:   "a title that is long enough to have crowded the time",
		Status:  store.Ready,
		Changed: testNow.Add(-2 * time.Hour),
	}

	lines := showTicketLines(t, ticket, "", time.Time{})
	if got := utf8.RuneCountInString(lines[0]); got > ruleWidth {
		t.Errorf("the heading is %d characters wide, want %d at most: %q", got, ruleWidth, lines[0])
	}
	if !strings.HasSuffix(lines[0], string(store.Ready)) {
		t.Errorf("the heading does not end with the status: %q", lines[0])
	}
	if want := strings.Repeat(" ", ruleWidth-len("2h ago")) + "2h ago"; lines[1] != want {
		t.Errorf("the line of the time is %q, want %q", lines[1], want)
	}
	if !strings.HasPrefix(strings.TrimSpace(lines[2]), "─") {
		t.Errorf("the rule does not come below the time: %q", lines[2])
	}
}

// Never show a ticking duration for a run that has ended.
func TestWriteTicketShowsNoDurationWhenTheTicketIsNotRunning(t *testing.T) {
	base := store.Ticket{
		ID: 9, Project: store.Project{Path: "/p/one"}, Title: "a title",
		Changed: testNow.Add(-2 * time.Hour),
	}
	started := testNow.Add(-(14*time.Minute + 7*time.Second))

	for _, status := range []store.TicketStatus{store.Ready, store.Queued, store.Failed} {
		ticket := base
		ticket.Status = status
		if got := timeOf(t, showTicketLines(t, ticket, "", started)); got == "00:14:07" {
			t.Errorf("a %s ticket gives the duration of a run: %q", status, got)
		}
	}
}

// Wrap metadata to the rule width while preserving description line breaks.
func TestWriteTicketKeepsEachFieldInsideTheRule(t *testing.T) {
	ticket := store.Ticket{
		ID: 4, Project: store.Project{Path: "/projects/a-name-of-some-length"}, Title: "a title", Status: store.Ready,
		Branch:  "delegator/4-a-title",
		Session: "e55e382e-2c88-4de7-a31d-ab8763a0fb5a",
	}
	for _, line := range showTicketLines(t, ticket, "", time.Time{}) {
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
	writeTicket(&out, shown{Ticket: store.Ticket{
		ID:      4,
		Project: store.Project{Path: repo},
		Title:   "a title",
		Status:  store.Ready,
		Commit:  hash,
	}, ProseFile: proseFile(t.TempDir(), 4)}, time.Now())

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

// An unresolvable commit must still expose its recorded hash.
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

// Verify dg show exposes the session ID while the agent is still running,
// using a fake that starts a session then waits.
func TestRunShowGivesTheSessionOfARunningTicket(t *testing.T) {
	dataDir := t.TempDir()
	_, id, repo := queuedTicket(t, dataDir)
	useFakeAgent(t, dataDir, "wait 1s", "stop end_turn")

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
		if strings.Contains(out, "session") && strings.Contains(out, "fake-1") {
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

// Field-only output must be raw, without labels, wrapping, or tilde
// abbreviation.
func TestWriteOnlyGivesOneField(t *testing.T) {
	ticket := store.Ticket{
		ID:      4,
		Project: store.Project{Path: "/projects/web-api"},
		Branch:  "delegator/4-remove-the-staging-app",
		Session: "e55e382e-2c88-4de7-a31d-ab8763a0fb5a",
	}
	tests := []struct {
		only onlyField
		want string
	}{
		{onlyProject, "/projects/web-api"},
		{onlyTicket, "/data/tickets/4.md"},
		{onlyWorktree, "/data/worktrees/4"},
		{onlyBranch, "delegator/4-remove-the-staging-app"},
		{onlySession, "e55e382e-2c88-4de7-a31d-ab8763a0fb5a"},
	}
	for _, test := range tests {
		var out bytes.Buffer
		writeOnly(&out, shown{
			Ticket:    ticket,
			ProseFile: proseFile("/data", ticket.ID),
			Worktree:  run.WorktreePath("/data", ticket.ID),
		}, test.only)
		if got, want := out.String(), test.want+"\n"; got != want {
			t.Errorf("--%s-only wrote %q, want %q", test.only, got, want)
		}
	}
}

func TestWriteOnlyWithNoValueWritesNothing(t *testing.T) {
	var out bytes.Buffer
	writeOnly(&out, shown{Ticket: store.Ticket{ID: 4}, ProseFile: proseFile("/data", 4)}, onlyWorktree)
	if got := out.String(); got != "" {
		t.Errorf("--worktree-only wrote %q for a worktree that is not on disk, want nothing", got)
	}
}

// Raw paths keep the full home directory; shells do not expand ~ from
// variables.
func TestWriteOnlyKeepsTheHomeOfAPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, "projects", "web-api")

	var out bytes.Buffer
	writeOnly(&out, shown{
		Ticket:    store.Ticket{ID: 4, Project: store.Project{Path: path}},
		ProseFile: proseFile("/data", 4),
	}, onlyProject)
	if got, want := out.String(), path+"\n"; got != want {
		t.Errorf("--project-only wrote %q, want %q", got, want)
	}
}

func TestRunShowWithAnOnlyFlagGivesTheFieldAlone(t *testing.T) {
	dataDir := t.TempDir()
	s, id, repo := readyTicket(t, dataDir)
	ticket, err := s.Ticket(id)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		flag string
		want string
	}{
		{"--project-only", repo},
		{"--ticket-only", proseFile(dataDir, id)},
		{"--worktree-only", run.WorktreePath(dataDir, id)},
		{"--branch-only", ticket.Branch},
	}
	for _, test := range tests {
		out, err := runIn(t, dataDir, repo, "show", fmt.Sprint(id), test.flag)
		if err != nil {
			t.Fatal(err)
		}
		if want := test.want + "\n"; out != want {
			t.Errorf("dg show %s wrote %q, want %q", test.flag, out, want)
		}
	}
}

// The first field-only flag wins in command-line order.
func TestRunShowWithTwoOnlyFlagsTakesTheFirst(t *testing.T) {
	dataDir := t.TempDir()
	s, id, repo := readyTicket(t, dataDir)
	ticket, err := s.Ticket(id)
	if err != nil {
		t.Fatal(err)
	}

	out, err := runIn(t, dataDir, repo, "show", fmt.Sprint(id), "--branch-only", "--project-only")
	if err != nil {
		t.Fatal(err)
	}
	if want := ticket.Branch + "\n"; out != want {
		t.Errorf("dg show --branch-only --project-only wrote %q, want %q", out, want)
	}

	out, err = runIn(t, dataDir, repo, "show", fmt.Sprint(id), "--project-only", "--branch-only")
	if err != nil {
		t.Fatal(err)
	}
	if want := repo + "\n"; out != want {
		t.Errorf("dg show --project-only --branch-only wrote %q, want %q", out, want)
	}
}

// Implicit selection follows ready position, not ID; finish the second-
// created ticket first.
func TestRunShowWithNoIDTakesTheHeadOfReady(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	testfix.CommitIn(t, repo, "first")
	s := testfix.OpenStore(t, dataDir)

	later := queuedIn(t, s, repo, "the ticket that finished last")
	head := queuedIn(t, s, repo, "the ticket that finished first")
	branch := finishIn(t, s, head)
	finishIn(t, s, later)
	assertReadyOrder(t, s, head, later)

	out, err := runIn(t, dataDir, repo, "show")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "the ticket that finished first") {
		t.Errorf("dg show with no id gave:\n%s\nwant the ticket %d", out, head)
	}

	out, err = runIn(t, dataDir, repo, "show", "--branch-only")
	if err != nil {
		t.Fatal(err)
	}
	if want := branch + "\n"; out != want {
		t.Errorf("dg show --branch-only with no id wrote %q, want %q", out, want)
	}
}

// twoProjects creates one ready ticket per project, with the other project
// finishing first to expose unscoped selection.
func twoProjects(t *testing.T) (dataDir, mine, other string, mineID, otherID int64) {
	t.Helper()
	dataDir = t.TempDir()
	mine = testfix.Repo(t, repoBranch)
	other = testfix.Repo(t, repoBranch)
	s := testfix.OpenStore(t, dataDir)

	otherID = queuedIn(t, s, other, "the ticket of the other project")
	mineID = queuedIn(t, s, mine, "the ticket of this project")
	finishIn(t, s, otherID)
	finishIn(t, s, mineID)
	return dataDir, mine, other, mineID, otherID
}

// Implicit selection must stay within the requested project.
func TestRunShowWithNoIDSkipsAnotherProject(t *testing.T) {
	dataDir, mine, _, _, _ := twoProjects(t)

	out, err := runIn(t, dataDir, mine, "show")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "the ticket of this project") {
		t.Errorf("dg show with no id gave:\n%s\nwant the ticket of the project it ran in", out)
	}
}

func TestRunShowWithNoIDTakesTheProjectOfTheFlag(t *testing.T) {
	dataDir, mine, other, _, _ := twoProjects(t)

	relative, err := filepath.Rel(mine, other)
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{other, relative} {
		out, err := runIn(t, dataDir, mine, "show", "--project", dir)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "the ticket of the other project") {
			t.Errorf("dg show --project %s gave:\n%s\nwant the ticket of that project", dir, out)
		}
	}
}

// Report no ready ticket for this project without selecting another project's
// work.
func TestRunShowWithNoIDAndNoReadyTicket(t *testing.T) {
	dataDir := t.TempDir()
	mine := testfix.Repo(t, repoBranch)
	other := testfix.Repo(t, repoBranch)
	s := testfix.OpenStore(t, dataDir)

	finishIn(t, s, queuedIn(t, s, other, "the ticket of the other project"))
	queuedIn(t, s, mine, "the ticket that waits for a run")

	out, err := runIn(t, dataDir, mine, "show")
	if err == nil {
		t.Fatalf("dg show with no ready ticket wrote:\n%s\nwant an error", out)
	}
	if !strings.Contains(err.Error(), "ready") {
		t.Errorf("err = %v, and it does not say that no ticket is ready", err)
	}
	if !strings.Contains(err.Error(), mine) {
		t.Errorf("err = %v, and it does not name the project %s", err, mine)
	}
	if out != "" {
		t.Errorf("dg show wrote %q, want nothing", out)
	}
}

func TestRunShowWithNoIDOutsideAProject(t *testing.T) {
	out, err := runIn(t, t.TempDir(), t.TempDir(), "show")
	if !errors.Is(err, project.ErrNotARepository) {
		t.Fatalf("err = %v, want ErrNotARepository", err)
	}
	if out != "" {
		t.Errorf("dg show wrote %q, want nothing", out)
	}
}

// Explicit IDs may select tickets from any project.
func TestRunShowWithAnIDTakesATicketOfAnotherProject(t *testing.T) {
	dataDir, mine, _, _, otherID := twoProjects(t)

	out, err := runIn(t, dataDir, mine, "show", fmt.Sprint(otherID))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "the ticket of the other project") {
		t.Errorf("dg show %d gave:\n%s\nwant the ticket of the other project", otherID, out)
	}
}

// RPC exposes structured ticket data without parsing terminal text.
func TestRunShowJSONHoldsEachField(t *testing.T) {
	dataDir := t.TempDir()
	s, id, repo := readyTicket(t, dataDir)
	if err := s.SetSession(id, "e55e382e-2c88-4de7-a31d-ab8763a0fb5a"); err != nil {
		t.Fatal(err)
	}
	const prose = "Remove the app, the volume and the records of the DNS."
	if err := os.WriteFile(proseFile(dataDir, id), []byte(prose+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ticket, err := s.Ticket(id)
	if err != nil {
		t.Fatal(err)
	}

	got := showJSON(t, dataDir, repo, fmt.Sprint(id))

	keys := slices.Sorted(maps.Keys(got))
	if want := slices.Sorted(slices.Values(jsonFields)); !slices.Equal(keys, want) {
		t.Errorf("RPC show result holds the keys %v, want %v", keys, want)
	}
	if got["id"] != float64(id) {
		t.Errorf("id = %v, want %d", got["id"], id)
	}
	want := map[string]any{
		"title":    ticket.Title,
		"status":   string(ticket.Status),
		"project":  repo,
		"ticket":   proseFile(dataDir, id),
		"worktree": run.WorktreePath(dataDir, id),
		"branch":   ticket.Branch,
		"session":  "e55e382e-2c88-4de7-a31d-ab8763a0fb5a",
		"commit":   ticket.Commit,
		"prose":    prose,
	}
	for key, want := range want {
		if got[key] != want {
			t.Errorf("%s = %v, want %v", key, got[key], want)
		}
	}
	if _, err := time.Parse(time.RFC3339, fmt.Sprint(got["created"])); err != nil {
		t.Errorf("created = %v, want a time of RFC 3339", got["created"])
	}
}

// JSON paths must remain absolute for use by other commands.
func TestRunShowJSONGivesTheFullPaths(t *testing.T) {
	dataDir := t.TempDir()
	_, id, repo := readyTicket(t, dataDir)
	// Use paths under home to exercise abbreviation boundaries.
	t.Setenv("HOME", filepath.Dir(dataDir))

	got := showJSON(t, dataDir, repo, fmt.Sprint(id))

	for _, key := range []string{"project", "ticket", "worktree"} {
		path, ok := got[key].(string)
		if !ok {
			t.Fatalf("%s = %v, want a path", key, got[key])
		}
		if !filepath.IsAbs(path) || strings.Contains(path, "~") {
			t.Errorf("%s = %q, want a full path with no tilde", key, path)
		}
	}
}

func TestRunShowJSONGivesNullForAFieldWithNoValue(t *testing.T) {
	dataDir := t.TempDir()
	_, id, repo := queuedTicket(t, dataDir)

	got := showJSON(t, dataDir, repo, fmt.Sprint(id))

	for _, key := range []string{"worktree", "branch", "session", "commit", "accepted"} {
		if value, held := got[key]; !held || value != nil {
			t.Errorf("%s = %v for a ticket in the queue, want null", key, value)
		}
	}
}

func TestRunShowJSONGivesTheTimeOfTheAcceptance(t *testing.T) {
	dataDir := t.TempDir()
	_, id, repo := readyTicket(t, dataDir)
	if _, err := runIn(t, dataDir, repo, "accept", fmt.Sprint(id)); err != nil {
		t.Fatal(err)
	}

	got := showJSON(t, dataDir, repo, fmt.Sprint(id))

	if _, err := time.Parse(time.RFC3339, fmt.Sprint(got["accepted"])); err != nil {
		t.Errorf("accepted = %v, want a time of RFC 3339", got["accepted"])
	}
}

// A missing ticket returns an RPC error without a ticket result.
func TestRunShowJSONWithATicketThatIsNotThere(t *testing.T) {
	out, err := rpcIn(t, t.TempDir(), testfix.Repo(t, repoBranch), `{"jsonrpc":"2.0","method":"show","params":{"args":[9999]},"id":1}`)
	if err != nil {
		t.Fatal(err)
	}
	response := rpcObject(t, out)
	if failure, ok := response["error"].(map[string]any); !ok || failure["code"] != float64(codeNoTicket) {
		t.Fatalf("response = %#v, want an error", response)
	}
	if _, ok := response["result"]; ok {
		t.Fatalf("response = %#v, want no result", response)
	}
}

// Leave <, >, and & unescaped so Markdown remains readable in JSON outside
// HTML.
func TestRunShowJSONKeepsTheCharactersOfTheProse(t *testing.T) {
	dataDir := t.TempDir()
	_, id, repo := queuedTicket(t, dataDir)
	const prose = "Take <staging> out of the DNS & the load balancer."
	if err := os.WriteFile(proseFile(dataDir, id), []byte(prose), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := rpcIn(t, dataDir, repo, fmt.Sprintf(`{"jsonrpc":"2.0","method":"show","params":{"args":[%d]},"id":1}`, id))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, prose) {
		t.Errorf("RPC show result does not hold the prose as it is:\n%s", out)
	}
}
