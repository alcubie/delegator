package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/run"
	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

// readyTicket makes a data directory holding one ticket that a run finished,
// which is the only state dg accept takes, with the worktree that the run left
// behind. It returns the store, the id of the ticket and the repository.
func readyTicket(t *testing.T, dataDir string) (*store.Store, int64, string) {
	t.Helper()
	s, ticketID, repo, commit := runningTicket(t, dataDir)

	ticket, err := s.Ticket(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	testfix.GitIn(t, repo, "worktree", "add", run.WorktreePath(dataDir, ticketID), ticket.Branch)

	if err := s.FinishTicket(ticketID, commit); err != nil {
		t.Fatal(err)
	}
	return s, ticketID, repo
}

func TestAcceptClosesAReadyTicket(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo := readyTicket(t, dataDir)

	out, err := runIn(t, dataDir, repo, "accept", fmt.Sprint(ticketID))
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("dg accept wrote %q, want nothing", out)
	}

	ticket, err := s.Ticket(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Status != store.Done {
		t.Errorf("status = %q, want %q", ticket.Status, store.Done)
	}
}

// Only a ready ticket is work that a person has read, so nothing else closes.
// The worktree must survive as well: an agent is still working in the one that
// belongs to a running ticket, and no removal can be undone.
func TestAcceptWithATicketThatIsNotReady(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo, _ := runningTicket(t, dataDir)

	ticket, err := s.Ticket(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	worktree := run.WorktreePath(dataDir, ticketID)
	testfix.GitIn(t, repo, "worktree", "add", worktree, ticket.Branch)

	if _, err := runIn(t, dataDir, repo, "accept", fmt.Sprint(ticketID)); err == nil {
		t.Fatal("err = nil, want ErrInvalidTicketStateChange")
	}

	if ticket, err = s.Ticket(ticketID); err != nil {
		t.Fatal(err)
	}
	if ticket.Status != store.Running {
		t.Errorf("status = %q, want %q", ticket.Status, store.Running)
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Errorf("the worktree of a running agent is gone: %v", err)
	}
}

// A person can remove a worktree by hand, and a command that failed after its
// own removal must be able to run again.
func TestAcceptWithTheWorktreeAlreadyGone(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo := readyTicket(t, dataDir)
	testfix.GitIn(t, repo, "worktree", "remove", run.WorktreePath(dataDir, ticketID))

	if _, err := runIn(t, dataDir, repo, "accept", fmt.Sprint(ticketID)); err != nil {
		t.Fatal(err)
	}

	ticket, err := s.Ticket(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Status != store.Done {
		t.Errorf("status = %q, want %q", ticket.Status, store.Done)
	}
}

// The worktree goes when the ticket closes, so the disk does not fill with a
// directory for each ticket a person ever accepted.
func TestAcceptRemovesTheWorktree(t *testing.T) {
	dataDir := t.TempDir()
	_, ticketID, repo := readyTicket(t, dataDir)

	worktree := run.WorktreePath(dataDir, ticketID)
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("the fixture made no worktree: %v", err)
	}

	if _, err := runIn(t, dataDir, repo, "accept", fmt.Sprint(ticketID)); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Errorf("the worktree is still at %s", worktree)
	}
	// git keeps its own record of a worktree, and one it still lists but cannot
	// find blocks the next worktree at that path.
	if out := testfix.GitOut(t, repo, "worktree", "list"); strings.Contains(out, worktree) {
		t.Errorf("git still lists the worktree:\n%s", out)
	}
}

// Git refuses a worktree holding changes that are not committed. The ticket
// must then stay ready, so a person sees the work again and dg accept can be
// run once the worktree is dealt with.
func TestAcceptWithAWorktreeGitWillNotRemove(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo := readyTicket(t, dataDir)

	stray := filepath.Join(run.WorktreePath(dataDir, ticketID), "not-committed.txt")
	if err := os.WriteFile(stray, []byte("work the agent left"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := runIn(t, dataDir, repo, "accept", fmt.Sprint(ticketID)); err == nil {
		t.Fatal("err = nil, want the refusal from git")
	}

	ticket, err := s.Ticket(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Status != store.Ready {
		t.Errorf("status = %q, want %q", ticket.Status, store.Ready)
	}
}

// The worktree goes but the branch stays, so a person can read the work again
// long after the ticket closed. dg show resolves the commit through it, and
// dg open diff needs it to exist.
func TestAcceptKeepsTheBranch(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo := readyTicket(t, dataDir)

	ticket, err := s.Ticket(ticketID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := runIn(t, dataDir, repo, "accept", fmt.Sprint(ticketID)); err != nil {
		t.Fatal(err)
	}

	if got := testfix.GitOut(t, repo, "branch", "--list", ticket.Branch); !strings.Contains(got, ticket.Branch) {
		t.Fatalf("git branch --list gives %q, and the branch of the run is gone", got)
	}
	if got := testfix.GitOut(t, repo, "rev-parse", ticket.Branch); got != ticket.Commit {
		t.Errorf("the branch is at %s, and the ticket holds %s", got, ticket.Commit)
	}
}

// The queue continues from the review. A ticket in ready holds it, so the
// command that closes the ticket is the one that starts the next.
func TestAcceptStartsTheNextTicket(t *testing.T) {
	dataDir := t.TempDir()
	_, ticketID, repo := readyTicket(t, dataDir)
	testfix.SecondTicket(t, dataDir)
	l, marker := testfix.RecordingLaunch(t)
	useLaunch(t, l)

	if _, err := runIn(t, dataDir, repo, "accept", fmt.Sprint(ticketID)); err != nil {
		t.Fatal(err)
	}

	testfix.WaitForStarts(t, marker, 1)
}

// The ticket a person accepts is nearly always the head of READY, so dg accept
// with no id takes it. The head is the ready ticket with the oldest
// completion, and it is not the smallest id: the ticket that finished first
// here is the second one made. The id goes out, because a command that closed
// a ticket the person did not name must say which one went.
func TestAcceptWithNoIDClosesTheHeadOfReady(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	s := testfix.OpenStore(t, dataDir)

	later := queuedIn(t, s, repo, "the ticket that finished last")
	head := queuedIn(t, s, repo, "the ticket that finished first")
	finishIn(t, s, head)
	nextSecond(t)
	finishIn(t, s, later)

	out, err := runIn(t, dataDir, repo, "accept")
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("%d\n", head); out != want {
		t.Errorf("dg accept with no id wrote %q, want %q", out, want)
	}

	if got := testfix.ReadTicket(t, dataDir, head).Status; got != store.Done {
		t.Errorf("the head of READY is %q, want %q", got, store.Done)
	}
	if got := testfix.ReadTicket(t, dataDir, later).Status; got != store.Ready {
		t.Errorf("the ticket that finished last is %q, want %q", got, store.Ready)
	}
}

// The head of READY is the head for one project. A person who closes the work
// of this repository must not close the ticket of another one, whatever the
// order of the inbox as a whole.
func TestAcceptWithNoIDSkipsAnotherProject(t *testing.T) {
	dataDir, mine, _, mineID, otherID := twoProjects(t)

	out, err := runIn(t, dataDir, mine, "accept")
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("%d\n", mineID); out != want {
		t.Errorf("dg accept with no id wrote %q, want %q", out, want)
	}

	if got := testfix.ReadTicket(t, dataDir, mineID).Status; got != store.Done {
		t.Errorf("the ticket of this project is %q, want %q", got, store.Done)
	}
	if got := testfix.ReadTicket(t, dataDir, otherID).Status; got != store.Ready {
		t.Errorf("the ticket of the other project is %q, want %q", got, store.Ready)
	}
}

// --project names the project, as it does on dg ticket, so a person closes the
// work of a repository from somewhere else. The path here is relative, which
// is the form that has a directory to be joined to.
func TestAcceptWithNoIDTakesTheProjectOfTheFlag(t *testing.T) {
	dataDir, mine, other, mineID, otherID := twoProjects(t)

	relative, err := filepath.Rel(mine, other)
	if err != nil {
		t.Fatal(err)
	}
	out, err := runIn(t, dataDir, mine, "accept", "--project", relative)
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("%d\n", otherID); out != want {
		t.Errorf("dg accept --project %s wrote %q, want %q", relative, out, want)
	}

	if got := testfix.ReadTicket(t, dataDir, otherID).Status; got != store.Done {
		t.Errorf("the ticket of the other project is %q, want %q", got, store.Done)
	}
	if got := testfix.ReadTicket(t, dataDir, mineID).Status; got != store.Ready {
		t.Errorf("the ticket of this project is %q, want %q", got, store.Ready)
	}
}

// A project with nothing ready has no ticket to close, and the error names it,
// because a person who gave --project may be looking at a project that is not
// the one they meant. Another project's ticket is not an answer to the
// question that was asked, and a ticket that no run has finished is not one
// either.
func TestAcceptWithNoIDAndNoReadyTicket(t *testing.T) {
	dataDir := t.TempDir()
	mine := testfix.Repo(t, repoBranch)
	other := testfix.Repo(t, repoBranch)
	s := testfix.OpenStore(t, dataDir)

	otherID := queuedIn(t, s, other, "the ticket of the other project")
	finishIn(t, s, otherID)
	queuedIn(t, s, mine, "the ticket that waits for a run")

	out, err := runIn(t, dataDir, mine, "accept")
	if err == nil {
		t.Fatalf("dg accept with no ready ticket wrote:\n%s\nwant an error", out)
	}
	if !strings.Contains(err.Error(), "ready") {
		t.Errorf("err = %v, and it does not say that no ticket is ready", err)
	}
	if !strings.Contains(err.Error(), mine) {
		t.Errorf("err = %v, and it does not name the project %s", err, mine)
	}
	if out != "" {
		t.Errorf("dg accept wrote %q, want nothing", out)
	}
	if got := testfix.ReadTicket(t, dataDir, otherID).Status; got != store.Ready {
		t.Errorf("the ticket of the other project is %q, want %q", got, store.Ready)
	}
}

// A directory outside any repository names no project, so there is no head of
// READY to close.
func TestAcceptWithNoIDOutsideAProject(t *testing.T) {
	out, err := runIn(t, t.TempDir(), t.TempDir(), "accept")
	if !errors.Is(err, project.ErrNotARepository) {
		t.Fatalf("err = %v, want ErrNotARepository", err)
	}
	if out != "" {
		t.Errorf("dg accept wrote %q, want nothing", out)
	}
}

// An id is still an id, and it names a ticket of any project: the id comes off
// the inbox, which is one list for every project.
func TestAcceptWithAnIDClosesATicketOfAnotherProject(t *testing.T) {
	dataDir, mine, _, mineID, otherID := twoProjects(t)

	out, err := runIn(t, dataDir, mine, "accept", fmt.Sprint(otherID))
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("dg accept %d wrote %q, want nothing", otherID, out)
	}

	if got := testfix.ReadTicket(t, dataDir, otherID).Status; got != store.Done {
		t.Errorf("the ticket of the other project is %q, want %q", got, store.Done)
	}
	if got := testfix.ReadTicket(t, dataDir, mineID).Status; got != store.Ready {
		t.Errorf("the ticket of this project is %q, want %q", got, store.Ready)
	}
}

// The id goes out after the ticket closes. A worktree git refuses leaves the
// ticket ready, and a line holding its id would tell the person that the
// ticket they never named is gone when it is still there.
func TestAcceptWithNoIDWritesNothingWhenTheCloseFails(t *testing.T) {
	dataDir := t.TempDir()
	_, ticketID, repo := readyTicket(t, dataDir)

	stray := filepath.Join(run.WorktreePath(dataDir, ticketID), "not-committed.txt")
	if err := os.WriteFile(stray, []byte("work the agent left"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := runIn(t, dataDir, repo, "accept")
	if err == nil {
		t.Fatal("err = nil, want the refusal from git")
	}
	if out != "" {
		t.Errorf("dg accept wrote %q, want nothing", out)
	}
	if got := testfix.ReadTicket(t, dataDir, ticketID).Status; got != store.Ready {
		t.Errorf("status = %q, want %q", got, store.Ready)
	}
}
