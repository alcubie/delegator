package store

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// readyTicket returns a store with one ready ticket and its ID, claimed so
// it has a run for Rerun to read the agent from.
func readyTicket(t *testing.T) (*Store, int64) {
	t.Helper()
	s, id := oneTicket(t)
	if _, err := s.Claim(id, "delegator/1-my-ticket", testAgentID); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeStatus(id, Ready); err != nil {
		t.Fatal(err)
	}
	return s, id
}

func TestRerunMakesAReadyTicketRunning(t *testing.T) {
	s, id := readyTicket(t)

	if _, err := s.Rerun(id, testAgentID, 4242); err != nil {
		t.Fatal(err)
	}

	ticket, err := s.Ticket(id)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Status != Running {
		t.Errorf("status = %q, want %q", ticket.Status, Running)
	}
}

// chat has already exited the process its PID named by the time reconcile
// next runs, so the new run must record exactly that PID rather than
// rerun's own.
func TestRerunRecordsTheGivenPIDOnTheNewRun(t *testing.T) {
	s, id := readyTicket(t)

	if _, err := s.Rerun(id, testAgentID, 4242); err != nil {
		t.Fatal(err)
	}

	run, err := s.Run(id)
	if err != nil {
		t.Fatal(err)
	}
	if run.PID != 4242 {
		t.Errorf("pid = %d, want 4242", run.PID)
	}
}

func TestRerunRecordsTheAgentOnTheNewRun(t *testing.T) {
	s, id := readyTicket(t)
	if err := s.SaveAgent(Agent{Name: "mine", Argv: []string{"mine-acp"}}); err != nil {
		t.Fatal(err)
	}
	agent, err := s.Agent("mine")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.Rerun(id, agent.ID, 4242); err != nil {
		t.Fatal(err)
	}

	run, err := s.Run(id)
	if err != nil {
		t.Fatal(err)
	}
	if run.AgentID != agent.ID || run.Agent != agent.Name {
		t.Errorf("agent = (%d, %q), want (%d, %q)", run.AgentID, run.Agent, agent.ID, agent.Name)
	}
}

func TestRerunGivesTheTicketASecondRun(t *testing.T) {
	s, id := readyTicket(t)
	first := runRows(t, s, id)

	if _, err := s.Rerun(id, testAgentID, 4242); err != nil {
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
		t.Error("the rerun wrote the row of the first run")
	}
}

func TestRerunRefusesEveryStatusButReady(t *testing.T) {
	for _, status := range []TicketStatus{Queued, Running, Failed, Done, Cancelled} {
		t.Run(string(status), func(t *testing.T) {
			s, id := oneTicket(t)
			if status != Queued {
				setStatus(t, s, id, status)
			}
			before := steps(t, s, id)

			_, err := s.Rerun(id, testAgentID, 4242)

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

func TestRerunNamesTheStatusItRefused(t *testing.T) {
	s, id := oneTicket(t)
	setStatus(t, s, id, Done)

	_, err := s.Rerun(id, testAgentID, 4242)

	if err == nil || !strings.Contains(err.Error(), string(Done)) {
		t.Errorf("err = %v, want it to name %q", err, Done)
	}
}

func TestRerunWithNoSuchTicket(t *testing.T) {
	s, _ := emptyStore(t)

	if _, err := s.Rerun(404, testAgentID, 4242); !errors.Is(err, ErrNoTicket) {
		t.Errorf("err = %v, want ErrNoTicket", err)
	}
}

func TestRerunWithNoSuchAgentKeepsTheTicketReady(t *testing.T) {
	s, id := readyTicket(t)
	if _, err := s.Rerun(id, 404, 4242); err == nil {
		t.Fatal("Rerun accepted an id that no agent has")
	}
	ticket, err := s.Ticket(id)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Status != Ready {
		t.Errorf("status = %q, want %q after the failed rerun", ticket.Status, Ready)
	}
}
