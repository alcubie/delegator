package cli

import (
	"fmt"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

// queuedTicket makes a data directory holding one project and one ticket in
// the queue, in a repository that has one commit. It returns the store, the id
// of the ticket and the repository.
func queuedTicket(t *testing.T, dataDir string) (*store.Store, int64, string) {
	t.Helper()
	repo := testfix.Repo(t, repoBranch)
	testfix.CommitIn(t, repo, "first")

	s := testfix.OpenStore(t, dataDir)
	projectID, err := s.AddProject(repo, repoBranch)
	if err != nil {
		t.Fatal(err)
	}
	ticketID, err := s.AddTicket(projectID, "ticket title")
	if err != nil {
		t.Fatal(err)
	}
	return s, ticketID, repo
}

// runningTicket makes a queued ticket and claims it for a run, on a branch
// that holds one commit. It returns the store, the id of the ticket, the
// repository and the hash of that commit.
func runningTicket(t *testing.T, dataDir string) (*store.Store, int64, string, string) {
	t.Helper()
	s, ticketID, repo := queuedTicket(t, dataDir)

	branch := fmt.Sprintf("delegator/%d-ticket-title", ticketID)
	testfix.GitIn(t, repo, "branch", branch)
	if _, err := s.Claim(ticketID, branch); err != nil {
		t.Fatal(err)
	}
	return s, ticketID, repo, testfix.GitOut(t, repo, "rev-parse", branch)
}

func TestFinishReadyTicket(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo, commit := runningTicket(t, dataDir)

	out, err := runIn(t, dataDir, repo, "finish", fmt.Sprint(ticketID), commit)
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("dg finish wrote %q, want nothing", out)
	}

	ticket, err := s.Ticket(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Status != store.Ready {
		t.Errorf("status = %s, want = ready", ticket.Status)
	}
	if ticket.Commit != commit {
		t.Errorf("commit_id = %s, want %s", ticket.Commit, commit)
	}
}

func TestFinishWritesTheTimeTheRunStopped(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo, commit := runningTicket(t, dataDir)

	if _, err := runIn(t, dataDir, repo, "finish", fmt.Sprint(ticketID), commit); err != nil {
		t.Fatal(err)
	}

	ticket, err := s.Ticket(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Completed.IsZero() {
		t.Fatal("completed is the zero time")
	}
	if since := time.Since(ticket.Completed); since < 0 || since > time.Minute {
		t.Errorf("completed = %v, and now is %v", ticket.Completed, time.Now())
	}
	// The column sorts as text, so the store holds the UTC form and not an
	// offset. A time read from that form carries UTC, and one read from an
	// offset would carry a fixed zone.
	if ticket.Completed.Location() != time.UTC {
		t.Errorf("completed = %v, want the UTC form", ticket.Completed)
	}
}

// The rebase onto the default branch is an instruction to the agent, and a run
// that ends behind the default branch all the same is a fact for the inbox to
// show, not an error here. dg finish must take the commit and make the ticket
// ready.
func TestFinishTakesACommitBehindTheDefaultBranch(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo, commit := runningTicket(t, dataDir)
	testfix.CommitIn(t, repo, "second")

	if _, err := runIn(t, dataDir, repo, "finish", fmt.Sprint(ticketID), commit); err != nil {
		t.Fatal(err)
	}

	ticket, err := s.Ticket(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Status != store.Ready {
		t.Errorf("status = %s, want = ready", ticket.Status)
	}
	if ticket.Commit != commit {
		t.Errorf("commit_id = %s, want %s", ticket.Commit, commit)
	}
}
