package cli

import (
	"fmt"
	"testing"

	"github.com/alcubie/delegator/internal/store"
)

// readyTicket makes a data directory holding one ticket that a run finished,
// which is the only state dg accept takes. It returns the store, the id of the
// ticket and the repository.
func readyTicket(t *testing.T, dataDir string) (*store.Store, int64, string) {
	t.Helper()
	s, ticketID, repo, commit := runningTicket(t, dataDir)
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
func TestAcceptWithATicketThatIsNotReady(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo, _ := runningTicket(t, dataDir)

	if _, err := runIn(t, dataDir, repo, "accept", fmt.Sprint(ticketID)); err == nil {
		t.Fatal("err = nil, want ErrInvalidTicketStateChange")
	}

	ticket, err := s.Ticket(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Status != store.Running {
		t.Errorf("status = %q, want %q", ticket.Status, store.Running)
	}
}
