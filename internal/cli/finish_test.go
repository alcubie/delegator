package cli

import (
	"fmt"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/store"
)

// runningTicket makes a data directory holding one project and one ticket that
// a run has taken, which is the only state dg finish accepts.
func runningTicket(t *testing.T, dataDir string) (*store.Store, int64) {
	t.Helper()
	s := openStore(t, dataDir)

	projectID, err := s.AddProject("/projects/path", "main")
	if err != nil {
		t.Fatal(err)
	}
	ticketID, err := s.AddTicket(projectID, "ticket title")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeStatus(ticketID, store.Running); err != nil {
		t.Fatal(err)
	}
	return s, ticketID
}

func TestFinishReadyTicket(t *testing.T) {
	dataDir := t.TempDir()
	repo := gitRepo(t)
	s, ticketID := runningTicket(t, dataDir)

	out, err := runIn(t, dataDir, repo, "finish", fmt.Sprint(ticketID), "123456")
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
	if ticket.Commit != "123456" {
		t.Errorf("commit_id = %s, want = 123456", ticket.Commit)
	}
}

func TestFinishWritesTheTimeTheRunStopped(t *testing.T) {
	dataDir := t.TempDir()
	repo := gitRepo(t)
	s, ticketID := runningTicket(t, dataDir)

	if _, err := runIn(t, dataDir, repo, "finish", fmt.Sprint(ticketID), "123456"); err != nil {
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
