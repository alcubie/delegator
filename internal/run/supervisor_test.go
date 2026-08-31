package run

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/alcubie/delegator/internal/adapters"
	"github.com/alcubie/delegator/internal/store"
)

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

	s, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	projectID, err := s.ProjectID(repo, "main")
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.AddTicket(projectID, title)
	if err != nil {
		t.Fatal(err)
	}

	gitIn(t, repo, "checkout", "-q", "-b", "other")
	commitIn(t, repo, "second")
	return dataDir, id
}

// readTicket returns one ticket from the data directory.
func readTicket(t *testing.T, dataDir string, id int64) store.Ticket {
	t.Helper()
	s, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ticket, err := s.Ticket(id)
	if err != nil {
		t.Fatal(err)
	}
	return ticket
}

func TestStartMakesTheWorktreeAndPutsTheTicketInRunning(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")

	if err := Start(dataDir, id, fakeAgent(t, "exit 0")); err != nil {
		t.Fatal(err)
	}

	ticket := readTicket(t, dataDir, id)
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
	if got := gitOut(t, worktree, "rev-parse", "--abbrev-ref", "HEAD"); got != want {
		t.Errorf("the worktree is on %q, want %q", got, want)
	}
	if got, base := gitOut(t, worktree, "rev-parse", "HEAD"), gitOut(t, ticket.Project.Path, "rev-parse", "main"); got != base {
		t.Errorf("the worktree starts at %s, want main at %s", got, base)
	}
}

// Section 5 lets a supervisor start the next ticket, so two can reach one
// ticket at the same time. The worktree is made first here, because a worktree
// that git refuses would hide which layer does the refusing: with it already
// there, only the database is left to say no.
func TestStartGivesOneTicketToOneRun(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	ticket := readTicket(t, dataDir, id)
	if _, err := Worktree(dataDir, ticket); err != nil {
		t.Fatal(err)
	}

	agent := fakeAgent(t, "exit 0")
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- Start(dataDir, id, agent)
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
	if got := readTicket(t, dataDir, id); got.Status != store.Running {
		t.Errorf("status = %q, want %q", got.Status, store.Running)
	}
}

// fakeAgent builds dg-fake-agent and returns an Adapter that runs the script.
func fakeAgent(t *testing.T, lines ...string) adapters.Fake {
	t.Helper()
	dir := t.TempDir()

	binary := filepath.Join(dir, "dg-fake-agent")
	out, err := exec.Command("go", "build", "-o", binary,
		"github.com/alcubie/delegator/cmd/dg-fake-agent").CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v: %s", err, out)
	}

	script := filepath.Join(dir, "script")
	if err := os.WriteFile(script, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return adapters.Fake{Binary: binary, Script: script}
}

// The script sleeps before it writes, so a Start that returns without waiting
// finds no file. The path is relative, so the file lands in the worktree only
// if the agent was started there.
func TestStartRunsTheAgentInTheWorktreeAndWaits(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	agent := fakeAgent(t, "run sleep 0.3", "write made-by-the-agent done", "exit 0")

	if err := Start(dataDir, id, agent); err != nil {
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
