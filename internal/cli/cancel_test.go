package cli

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/run"
	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

// failedTicket makes a data directory holding one ticket whose run stopped
// with no report, which is the state the supervisor and the reconcile both
// leave behind. It returns the store, the id of the ticket and the repository.
func failedTicket(t *testing.T, dataDir string) (*store.Store, int64, string) {
	t.Helper()
	s, ticketID, repo, _ := runningTicket(t, dataDir)
	if err := s.ChangeStatus(ticketID, store.Failed); err != nil {
		t.Fatal(err)
	}
	return s, ticketID, repo
}

// cancelIn runs dg cancel on one ticket and stops the test if the command
// gives an error. The command writes nothing: the person named the ticket, so
// there is nothing to tell them that they do not know.
func cancelIn(t *testing.T, dataDir, workDir string, id int64) {
	t.Helper()
	out, err := runIn(t, dataDir, workDir, "cancel", fmt.Sprint(id))
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("dg cancel wrote %q, want nothing", out)
	}
}

// A person who queued a ticket by mistake, or who read a run that went wrong,
// closes it with dg cancel. The three states below hold no run to stop.
func TestCancelClosesATicketThatIsNotRunning(t *testing.T) {
	tests := []struct {
		state string
		setUp func(t *testing.T, dataDir string) (*store.Store, int64, string)
	}{
		{"queued", queuedTicket},
		{"ready", readyTicket},
		{"failed", failedTicket},
	}
	for _, test := range tests {
		t.Run(test.state, func(t *testing.T) {
			dataDir := t.TempDir()
			_, ticketID, repo := test.setUp(t, dataDir)

			cancelIn(t, dataDir, repo, ticketID)

			if got := testfix.ReadTicket(t, dataDir, ticketID).Status; got != store.Cancelled {
				t.Errorf("status = %q, want %q", got, store.Cancelled)
			}
		})
	}
}

// A ticket that leaves the queue leaves the order of the tickets behind it
// where it was. The person cancelled one ticket and not the order of the rest.
func TestCancelTakesAQueuedTicketOffTheQueue(t *testing.T) {
	dataDir, repo, ids := threeInTheQueue(t)

	cancelIn(t, dataDir, repo, ids[0])

	want := []string{"second", "third"}
	if got := queueTitlesOf(t, dataDir); !slices.Equal(got, want) {
		t.Errorf("the queue is %v, want %v", got, want)
	}
}

// done and cancelled are the end. The state machine refuses the change, so a
// ticket that is closed does not reopen and a second dg cancel is the same
// refusal as the first.
func TestCancelRefusesATicketThatIsClosed(t *testing.T) {
	for _, state := range []store.TicketStatus{store.Done, store.Cancelled} {
		t.Run(string(state), func(t *testing.T) {
			dataDir := t.TempDir()
			s, ticketID, repo := readyTicket(t, dataDir)
			if err := s.ChangeStatus(ticketID, state); err != nil {
				t.Fatal(err)
			}

			_, err := runIn(t, dataDir, repo, "cancel", fmt.Sprint(ticketID))
			if !errors.Is(err, store.ErrInvalidTicketStateChange) {
				t.Fatalf("err = %v, want ErrInvalidTicketStateChange", err)
			}
			if got := testfix.ReadTicket(t, dataDir, ticketID).Status; got != state {
				t.Errorf("status = %q, want %q", got, state)
			}
		})
	}
}

// The stop is a signal to the process group of the supervisor, so it reaches
// the agent and each program the agent started, and not the supervisor alone.
func TestCancelStopsTheRun(t *testing.T) {
	dataDir := t.TempDir()
	_, ticketID, repo, _ := runningTicket(t, dataDir)
	pgid := testfix.LiveRun(t, dataDir, ticketID)

	cancelIn(t, dataDir, repo, ticketID)

	testfix.WaitForGroupGone(t, pgid)
	if got := testfix.ReadTicket(t, dataDir, ticketID).Status; got != store.Cancelled {
		t.Errorf("status = %q, want %q", got, store.Cancelled)
	}
}

// The supervisor that the signal ended writes nothing, so the command writes
// the end of the run. A run with no end time would look like one that is still
// going.
func TestCancelWritesTheEndOfTheRunItStopped(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo, _ := runningTicket(t, dataDir)
	testfix.LiveRun(t, dataDir, ticketID)
	before := time.Now().UTC().Truncate(time.Second)

	cancelIn(t, dataDir, repo, ticketID)

	stopped, err := s.Run(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.EndedAt.Before(before) || stopped.EndedAt.After(time.Now()) {
		t.Errorf("ended at = %s, want between %s and now", stopped.EndedAt, before)
	}
}

// A run that the reconcile calls dead gets no signal. Its process id can
// belong to a different program by then, and the run below is past the
// timeout with a process id that is very much alive. The reconcile of the
// command has already made the ticket failed, and the cancel writes the state
// alone.
func TestCancelSendsNoSignalToARunTheReconcileCallsDead(t *testing.T) {
	dataDir := t.TempDir()
	_, ticketID, repo, _ := runningTicket(t, dataDir)
	pgid := testfix.LiveRun(t, dataDir, ticketID)
	testfix.AgeRun(t, dataDir, ticketID, 2*time.Hour)

	cancelIn(t, dataDir, repo, ticketID)

	if !testfix.GroupAlive(pgid) {
		t.Error("the process group of the dead run was signalled")
	}
	if got := testfix.ReadTicket(t, dataDir, ticketID).Status; got != store.Cancelled {
		t.Errorf("status = %q, want %q", got, store.Cancelled)
	}
}

// The supervisor that a cancel stops is the program that would have started
// the next run when its own ended. The command starts it in the supervisor's
// place, so a cancel frees the queue rather than holding it until the person
// types another command.
func TestCancelStartsTheNextRun(t *testing.T) {
	dataDir := t.TempDir()
	_, ticketID, repo, _ := runningTicket(t, dataDir)
	testfix.LiveRun(t, dataDir, ticketID)
	testfix.SecondTicket(t, dataDir)
	l, marker := testfix.RecordingLaunch(t)
	useLaunch(t, l)

	cancelIn(t, dataDir, repo, ticketID)

	testfix.WaitFor(t, marker)
}

// The stop is not something a person types by hand, so dg cancel takes the id
// of the ticket and nothing else.
func TestCancelWithNoID(t *testing.T) {
	if _, err := runIn(t, t.TempDir(), t.TempDir(), "cancel"); err == nil {
		t.Error("err = nil, want dg cancel to ask for an id")
	}
}

// A word that is not a number is not the id of a ticket, and the command says
// so rather than acting on a ticket the person did not name.
func TestCancelWithAnIDThatIsNotANumber(t *testing.T) {
	_, err := runIn(t, t.TempDir(), t.TempDir(), "cancel", "seven")
	if err == nil || !strings.Contains(err.Error(), "seven") {
		t.Errorf("err = %v, want it to name seven", err)
	}
}

// A ticket in running keeps its worktree. The work of a run that a person
// stopped may be work they want to read, and no removal can be undone;
// dg accept is what removes a worktree, and only for work that is complete.
func TestCancelKeepsTheWorktree(t *testing.T) {
	dataDir := t.TempDir()
	_, ticketID, repo, _ := runningTicket(t, dataDir)
	worktree := run.WorktreePath(dataDir, ticketID)
	testfix.GitIn(t, repo, "worktree", "add", worktree, testfix.ReadTicket(t, dataDir, ticketID).Branch)
	testfix.LiveRun(t, dataDir, ticketID)

	cancelIn(t, dataDir, repo, ticketID)

	if _, err := os.Stat(worktree); err != nil {
		t.Errorf("the worktree of the cancelled run is gone: %v", err)
	}
}
