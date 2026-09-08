package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

	testfix.WaitFor(t, marker)
}
