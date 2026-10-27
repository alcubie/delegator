package cli

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/run"
	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

// withChatPID sets the environment variable dg ticket chat leaves on the
// agent it starts, naming its own process.
func withChatPID(t *testing.T, pid int) {
	t.Helper()
	t.Setenv(run.ChatPIDEnvironment, strconv.Itoa(pid))
}

func TestRerunMarksTheTicketRunningUsingTheChatPID(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo := readyTicket(t, dataDir)
	prior, err := s.Run(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	withChatPID(t, os.Getpid())

	out, err := runIn(t, dataDir, repo, "ticket", "rerun", fmt.Sprint(ticketID))
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("dg ticket rerun wrote %q, want nothing", out)
	}

	ticket, err := s.Ticket(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Status != store.Running {
		t.Errorf("status = %q, want %q", ticket.Status, store.Running)
	}
	newRun, err := s.Run(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if newRun.ID == prior.ID {
		t.Error("dg ticket rerun did not record a new run")
	}
	if newRun.PID != os.Getpid() {
		t.Errorf("pid = %d, want the chat process's %d", newRun.PID, os.Getpid())
	}
	if newRun.AgentID != prior.AgentID {
		t.Errorf("agent = %d, want the prior run's %d", newRun.AgentID, prior.AgentID)
	}
}

func TestRerunRefusesWithoutTheChatEnvironment(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo := readyTicket(t, dataDir)
	t.Setenv(run.ChatPIDEnvironment, "")

	_, err := runIn(t, dataDir, repo, "ticket", "rerun", fmt.Sprint(ticketID))

	if err == nil || !strings.Contains(err.Error(), "dg ticket chat") {
		t.Fatalf("err = %v, want an error naming dg ticket chat", err)
	}
	if got, readErr := s.Ticket(ticketID); readErr != nil || got.Status != store.Ready {
		t.Errorf("status = %q (err %v), want %q unchanged", got.Status, readErr, store.Ready)
	}
}

func TestRerunRefusesANonNumericChatPID(t *testing.T) {
	dataDir := t.TempDir()
	_, ticketID, repo := readyTicket(t, dataDir)
	t.Setenv(run.ChatPIDEnvironment, "not-a-pid")

	_, err := runIn(t, dataDir, repo, "ticket", "rerun", fmt.Sprint(ticketID))

	if err == nil || !strings.Contains(err.Error(), "not-a-pid") {
		t.Fatalf("err = %v, want an error naming the invalid value", err)
	}
}

func TestRerunRefusesATicketThatIsNotReady(t *testing.T) {
	tests := []struct {
		state string
		setUp func(t *testing.T, dataDir string) (*store.Store, int64, string)
	}{
		{"queued", queuedTicket},
		{"running", func(t *testing.T, dataDir string) (*store.Store, int64, string) {
			s, id, repo, _ := runningTicket(t, dataDir)
			return s, id, repo
		}},
		{"failed", failedTicket},
		{"done", func(t *testing.T, dataDir string) (*store.Store, int64, string) {
			s, id, repo := readyTicket(t, dataDir)
			if err := s.ChangeStatus(id, store.Done); err != nil {
				t.Fatal(err)
			}
			return s, id, repo
		}},
		{"cancelled", func(t *testing.T, dataDir string) (*store.Store, int64, string) {
			s, id, repo := queuedTicket(t, dataDir)
			if err := s.ChangeStatus(id, store.Cancelled); err != nil {
				t.Fatal(err)
			}
			return s, id, repo
		}},
	}
	for _, test := range tests {
		t.Run(test.state, func(t *testing.T) {
			dataDir := t.TempDir()
			_, ticketID, repo := test.setUp(t, dataDir)
			before := testfix.ReadTicket(t, dataDir, ticketID)
			withChatPID(t, os.Getpid())

			_, err := runIn(t, dataDir, repo, "ticket", "rerun", fmt.Sprint(ticketID))

			if !errors.Is(err, store.ErrInvalidTicketStateChange) {
				t.Fatalf("err = %v, want ErrInvalidTicketStateChange", err)
			}
			if got := testfix.ReadTicket(t, dataDir, ticketID); got.Status != before.Status {
				t.Errorf("status = %q, want %q", got.Status, before.Status)
			}
		})
	}
}

// Keep the entry point for the agent inside dg ticket chat hidden from
// ordinary help but callable directly.
func TestRerunIsHiddenFromTheHelp(t *testing.T) {
	for _, group := range Root(t.TempDir()).Commands() {
		if group.Name() != "ticket" {
			continue
		}
		for _, c := range group.Commands() {
			if c.Name() == "rerun" {
				if !c.Hidden {
					t.Error("dg ticket rerun is in the help")
				}
				return
			}
		}
	}
	t.Error("dg ticket rerun is not in the command tree")
}
