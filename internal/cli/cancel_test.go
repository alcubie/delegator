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

// failedTicket creates an unreported failed run, returning the store, ticket
// ID, and repository.
func failedTicket(t *testing.T, dataDir string) (*store.Store, int64, string) {
	t.Helper()
	s, ticketID, repo, _ := runningTicket(t, dataDir)
	if err := s.ChangeStatus(ticketID, store.Failed); err != nil {
		t.Fatal(err)
	}
	return s, ticketID, repo
}

// cancelIn runs dg cancel and checks that it succeeds silently.
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

// These states have no live run to stop.
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

// Cancellation must preserve the remaining queue order.
func TestCancelTakesAQueuedTicketOffTheQueue(t *testing.T) {
	dataDir, repo, ids := threeInTheQueue(t)

	cancelIn(t, dataDir, repo, ids[0])

	want := []string{"second", "third"}
	if got := queueTitlesOf(t, dataDir); !slices.Equal(got, want) {
		t.Errorf("the queue is %v, want %v", got, want)
	}
}

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

// Terminate the whole supervisor group, including its agent.
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

// Cancellation must close the run if the stopped supervisor could not.
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

// Use an expired run with a live PID to verify reconciliation prevents
// signalling a potentially reused process.
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

// Cancellation must replace the stopped supervisor's scheduling trigger.
func TestCancelStartsTheNextRun(t *testing.T) {
	dataDir := t.TempDir()
	_, ticketID, repo, _ := runningTicket(t, dataDir)
	testfix.LiveRun(t, dataDir, ticketID)
	testfix.SecondTicket(t, dataDir)
	l, record := testfix.RecordingLaunch(t)
	useLaunch(t, l)

	cancelIn(t, dataDir, repo, ticketID)

	testfix.WaitForStarts(t, record, 1)
}

func TestCancelWithNoID(t *testing.T) {
	if _, err := runIn(t, t.TempDir(), t.TempDir(), "cancel"); err == nil {
		t.Error("err = nil, want dg cancel to ask for an id")
	}
}

func TestCancelWithAnIDThatIsNotANumber(t *testing.T) {
	_, err := runIn(t, t.TempDir(), t.TempDir(), "cancel", "seven")
	if err == nil || !strings.Contains(err.Error(), "seven") {
		t.Errorf("err = %v, want it to name seven", err)
	}
}

// Preserve cancelled worktrees for inspection and recovery.
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
