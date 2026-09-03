package run

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/alcubie/delegator/internal/adapters"
	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

func TestMain(m *testing.M) {
	os.Exit(testfix.RunTests(m, false))
}

// queuedTicket makes a data directory that holds one project and one ticket in
// the queue, and returns the directory and the id of the ticket.
//
// It leaves the repository with HEAD on a branch that is not main, which is
// where a person usually is. A run that starts from HEAD rather than from the
// default branch therefore fails a test instead of passing on a fixture that
// has one branch.
func queuedTicket(t *testing.T, title string) (string, int64) {
	t.Helper()
	repo := repoOnMain(t)
	dataDir := t.TempDir()

	s := testfix.OpenStore(t, dataDir)

	projectID, err := s.ProjectID(repo, "main")
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.AddTicket(projectID, title)
	if err != nil {
		t.Fatal(err)
	}

	testfix.GitIn(t, repo, "checkout", "-q", "-b", "other")
	testfix.CommitIn(t, repo, "second")
	return dataDir, id
}

func TestStartMakesTheWorktreeAndPutsTheTicketInRunning(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")

	if err := Start(testfix.OpenStore(t, dataDir), id, fakeAgent(t, "exit 0")); err != nil {
		t.Fatal(err)
	}

	ticket := testfix.ReadTicket(t, dataDir, id)
	if ticket.Status != store.Running {
		t.Errorf("status = %q, want %q", ticket.Status, store.Running)
	}

	want := branch(id, "Add the thing")
	if ticket.Branch != want {
		t.Errorf("branch = %q, want %q", ticket.Branch, want)
	}

	worktree := filepath.Join(dataDir, "worktrees", "1")
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("the worktree is not there: %v", err)
	}
	if got := testfix.GitOut(t, worktree, "rev-parse", "--abbrev-ref", "HEAD"); got != want {
		t.Errorf("the worktree is on %q, want %q", got, want)
	}
	if got, base := testfix.GitOut(t, worktree, "rev-parse", "HEAD"), testfix.GitOut(t, ticket.Project.Path, "rev-parse", "main"); got != base {
		t.Errorf("the worktree starts at %s, want main at %s", got, base)
	}
}

// A supervisor starts the next ticket when its own run ends, so two can reach
// one ticket at the same time. The worktree is made first here, because a worktree
// that git refuses would hide which layer does the refusing: with it already
// there, only the database is left to say no.
func TestStartGivesOneTicketToOneRun(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	ticket := testfix.ReadTicket(t, dataDir, id)
	if _, err := Worktree(dataDir, ticket); err != nil {
		t.Fatal(err)
	}

	agent := fakeAgent(t, "exit 0")
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		s := testfix.OpenStore(t, dataDir)
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- Start(s, id, agent)
		}()
	}
	wg.Wait()
	close(errs)

	won := 0
	for err := range errs {
		switch {
		case err == nil:
			won++
		case !errors.Is(err, store.ErrInvalidTicketStateChange):
			t.Errorf("err = %v, want ErrInvalidTicketStateChange", err)
		}
	}
	if won != 1 {
		t.Errorf("%d supervisors took the ticket, want 1", won)
	}
	if got := testfix.ReadTicket(t, dataDir, id); got.Status != store.Running {
		t.Errorf("status = %q, want %q", got.Status, store.Running)
	}
}

// fakeAgent returns an Adapter that runs the given script. It is here and not
// in testfix because testfix cannot import adapters.
func fakeAgent(t *testing.T, lines ...string) adapters.Fake {
	t.Helper()
	return adapters.Fake{Binary: testfix.FakeAgentPath, Script: testfix.Script(t, lines...)}
}

// The script sleeps before it writes, so a Start that returns without waiting
// finds no file. The path is relative, so the file lands in the worktree only
// if the agent was started there.
func TestStartRunsTheAgentInTheWorktreeAndWaits(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	agent := fakeAgent(t, "run sleep 0.3", "write made-by-the-agent done", "exit 0")

	if err := Start(testfix.OpenStore(t, dataDir), id, agent); err != nil {
		t.Fatal(err)
	}

	made := filepath.Join(dataDir, "worktrees", "1", "made-by-the-agent")
	content, err := os.ReadFile(made)
	if err != nil {
		t.Fatalf("the agent did not finish before Start returned: %v", err)
	}
	if got := string(content); got != "done" {
		t.Errorf("content = %q, want %q", got, "done")
	}
}

// logOf returns the one log a run wrote below runs/<id>.
func logOf(t *testing.T, dataDir string, id int64) string {
	t.Helper()
	logs, err := filepath.Glob(filepath.Join(dataDir, "runs", strconv.FormatInt(id, 10), "*.log"))
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 {
		t.Fatalf("logs = %v, want one", logs)
	}
	data, err := os.ReadFile(logs[0])
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestStartRecordsTheSessionTheRunReported(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	agent := fakeAgent(t, "run printf 'session: s-1\\n'", "exit 0")

	if err := Start(testfix.OpenStore(t, dataDir), id, agent); err != nil {
		t.Fatal(err)
	}

	if got := testfix.ReadTicket(t, dataDir, id).Session; got != "s-1" {
		t.Errorf("session = %q, want %q", got, "s-1")
	}
}

// The run that fails is the one a person most wants to open, so its session
// is recorded before its failure is reported.
func TestStartRecordsTheSessionOfARunThatFailed(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	agent := fakeAgent(t, "run printf 'session: s-1\\n'", "exit 3")

	if err := Start(testfix.OpenStore(t, dataDir), id, agent); err == nil {
		t.Fatal("err = nil, want the exit status of the run")
	}

	if got := testfix.ReadTicket(t, dataDir, id).Session; got != "s-1" {
		t.Errorf("session = %q, want %q", got, "s-1")
	}
}

// The log holds what the agent wrote on both streams, so a person can read a
// run that produced no commit.
func TestStartWritesTheLogOfTheRun(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	agent := fakeAgent(t, "run printf 'to stdout\\n'", "run printf 'to stderr\\n' >&2", "exit 0")

	if err := Start(testfix.OpenStore(t, dataDir), id, agent); err != nil {
		t.Fatal(err)
	}

	log := logOf(t, dataDir, id)
	for _, want := range []string{"to stdout", "to stderr"} {
		if !strings.Contains(log, want) {
			t.Errorf("the log does not hold %q:\n%s", want, log)
		}
	}
}

// The agent learns the ticket through dg show and ends through dg finish,
// so the prompt must name both with the right id.
func TestPromptNamesBothCommands(t *testing.T) {
	got := prompt(42)
	for _, want := range []string{"dg show 42", "dg finish 42"} {
		if !strings.Contains(got, want) {
			t.Errorf("the prompt does not hold %q:\n%s", want, got)
		}
	}
}
