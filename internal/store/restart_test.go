package store

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// failedTicket returns a store with one failed ticket and its ID.
func failedTicket(t *testing.T) (*Store, int64) {
	t.Helper()
	s, id := oneTicket(t)
	if _, err := s.Claim(id, "delegator/1-my-ticket"); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeStatus(id, Failed); err != nil {
		t.Fatal(err)
	}
	return s, id
}

// Restart claims directly without disturbing queue positions.
func TestRestartMakesAFailedTicketRunning(t *testing.T) {
	s, id := failedTicket(t)
	waiting := queuedTickets(t, s, mustProject(t, s), "the second")

	if _, err := s.Restart(id); err != nil {
		t.Fatal(err)
	}

	ticket, err := s.Ticket(id)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Status != Running {
		t.Errorf("status = %q, want %q", ticket.Status, Running)
	}
	if got := queueTitles(t, s); !slices.Equal(got, []string{"the second"}) {
		t.Errorf("the queue is %v, want only the ticket that was waiting", got)
	}
	if got := ticketPosition(t, s, "position", waiting[0]); !got.Valid {
		t.Error("the ticket that was waiting lost its place in the queue")
	}
}

// Preserve the branch and session to continue the failed run's work.
func TestRestartKeepsTheBranchAndTheSession(t *testing.T) {
	s, id := failedTicket(t)
	if err := s.SetSession(id, "s-1"); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Restart(id); err != nil {
		t.Fatal(err)
	}

	ticket, err := s.Ticket(id)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Session != "s-1" {
		t.Errorf("session = %q, want %q", ticket.Session, "s-1")
	}
	if ticket.Branch != "delegator/1-my-ticket" {
		t.Errorf("branch = %q, want %q", ticket.Branch, "delegator/1-my-ticket")
	}
}

// Restart accepts only Failed, even though other state transitions are valid
// elsewhere.
func TestRestartRefusesEveryStatusButFailed(t *testing.T) {
	for _, status := range []TicketStatus{Queued, Running, Ready, Done, Cancelled} {
		t.Run(string(status), func(t *testing.T) {
			s, id := oneTicket(t)
			if status != Queued {
				setStatus(t, s, id, status)
			}
			before := steps(t, s, id)

			_, err := s.Restart(id)

			if !errors.Is(err, ErrInvalidTicketStateChange) {
				t.Fatalf("err = %v, want ErrInvalidTicketStateChange", err)
			}
			ticket, readErr := s.Ticket(id)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if ticket.Status != status {
				t.Errorf("status = %q, want %q", ticket.Status, status)
			}
			if got := steps(t, s, id); !slices.Equal(got, before) {
				t.Errorf("the history is %v, want %v", got, before)
			}
		})
	}
}

func TestRestartNamesTheStatusItRefused(t *testing.T) {
	s, id := oneTicket(t)
	setStatus(t, s, id, Done)

	_, err := s.Restart(id)

	if err == nil || !strings.Contains(err.Error(), string(Done)) {
		t.Errorf("err = %v, want it to name %q", err, Done)
	}
}

func TestRestartWithNoSuchTicket(t *testing.T) {
	s, _ := emptyStore(t)

	if _, err := s.Restart(404); !errors.Is(err, ErrNoTicket) {
		t.Errorf("err = %v, want ErrNoTicket", err)
	}
}

// Restart creates a new run without modifying the failed run's record.
func TestRestartGivesTheTicketASecondRun(t *testing.T) {
	s, id := failedTicket(t)
	first := runRows(t, s, id)

	if _, err := s.Restart(id); err != nil {
		t.Fatal(err)
	}

	runs := runRows(t, s, id)
	if len(runs) != 2 {
		t.Fatalf("the ticket has %d runs, want 2", len(runs))
	}
	if runs[0] != first[0] {
		t.Errorf("the first run is now %+v, want %+v", runs[0], first[0])
	}
	if runs[1].id == runs[0].id {
		t.Error("the second claim wrote the row of the first run")
	}
}
