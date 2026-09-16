package cli

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/project"
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

func TestFinishRefusesACommitGitDoesNotKnow(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo, _ := runningTicket(t, dataDir)
	unknown := strings.Repeat("0", 40)

	_, err := runIn(t, dataDir, repo, "finish", fmt.Sprint(ticketID), unknown)
	if !errors.Is(err, project.ErrUnknownCommit) {
		t.Errorf("err = %v, want ErrUnknownCommit", err)
	}

	ticket, err := s.Ticket(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Status != store.Running {
		t.Errorf("status = %s, want running", ticket.Status)
	}
	if ticket.Commit != "" {
		t.Errorf("commit_id = %q, want none", ticket.Commit)
	}
}

func TestFinishRefusesACommitTheTicketBranchDoesNotHold(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo, _ := runningTicket(t, dataDir)
	testfix.CommitIn(t, repo, "commit outside the ticket branch")
	other := testfix.GitOut(t, repo, "rev-parse", "HEAD")

	_, err := runIn(t, dataDir, repo, "finish", fmt.Sprint(ticketID), other)
	if !errors.Is(err, project.ErrCommitNotOnBranch) {
		t.Errorf("err = %v, want ErrCommitNotOnBranch", err)
	}

	ticket, err := s.Ticket(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Status != store.Running {
		t.Errorf("status = %s, want running", ticket.Status)
	}
	if ticket.Commit != "" {
		t.Errorf("commit_id = %q, want none", ticket.Commit)
	}
}

func TestFinishWritesTheFullHashWhenGivenAShortHash(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo, commit := runningTicket(t, dataDir)

	if _, err := runIn(t, dataDir, repo, "finish", fmt.Sprint(ticketID), commit[:7]); err != nil {
		t.Fatal(err)
	}

	ticket, err := s.Ticket(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Commit != commit {
		t.Errorf("commit_id = %q, want full hash %q", ticket.Commit, commit)
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
	if ticket.Changed.IsZero() {
		t.Fatal("the time of the change is the zero time")
	}
	if since := time.Since(ticket.Changed); since < 0 || since > time.Minute {
		t.Errorf("the ticket changed at %v, and now is %v", ticket.Changed, time.Now())
	}
	// The column sorts as text, so the store holds the UTC form and not an
	// offset. A time read from that form carries UTC, and one read from an
	// offset would carry a fixed zone.
	if ticket.Changed.Location() != time.UTC {
		t.Errorf("the ticket changed at %v, want the UTC form", ticket.Changed)
	}
}
