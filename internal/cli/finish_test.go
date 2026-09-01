package cli

import (
	"fmt"
	"testing"

	"github.com/alcubie/delegator/internal/store"
)

func TestFinishReadyTicket(t *testing.T) {
	dataDir := t.TempDir()
	repo := gitRepo(t)

	s := openStore(t, dataDir)
	projectID, err := s.AddProject("/projects/path", "main")
	if err != nil {
		t.Fatal(err)
	}
	ticketID, err := s.AddTicket(projectID, "ticket title")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ChangeStatus(ticketID, store.Running); err != nil {
		t.Fatal(err)
	}

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
