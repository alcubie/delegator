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
	if err := s.Claim(ticketID, branch); err != nil {
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
	completed, err := time.Parse(time.RFC3339, ticket.Completed)
	if err != nil {
		t.Fatalf("completed = %q, which is not RFC 3339: %v", ticket.Completed, err)
	}
	if since := time.Since(completed); since < 0 || since > time.Minute {
		t.Errorf("completed = %v, and now is %v", completed, time.Now())
	}
	// inbox.byCompletion compares the strings, so a time held with an offset
	// instead of Z sorts against a UTC time by its digits and not its instant.
	if want := completed.UTC().Format(time.RFC3339); ticket.Completed != want {
		t.Errorf("completed = %q, want the UTC form %q", ticket.Completed, want)
	}
}
