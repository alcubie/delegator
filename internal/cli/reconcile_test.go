package cli

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

// staleTicket creates a dead supervisor record followed by queued work,
// returning the repository and both ticket IDs.
func staleTicket(t *testing.T, dataDir string) (repo string, stale, queued int64) {
	t.Helper()
	_, stale, repo, _ = runningTicket(t, dataDir)
	testfix.StaleRun(t, dataDir, stale)
	return repo, stale, testfix.SecondTicket(t, dataDir)
}

// Every stateful command must recover stale runs before its own work so dead
// supervisors cannot keep occupying capacity.
func TestEachCommandDoesTheReconcile(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"list"},
		{"show", "1"},
		{"move", "2", "top"},
		{"ticket", "create", "Add rate limiting", "--no-body"},
		{"finish", "1", "abc123"},
		{"accept", "1"},
		{"pause"},
		{"start"},
		{"run", "2"},
	} {
		name := strings.Join(args, " ")
		if name == "" {
			name = "the inbox"
		}
		t.Run(name, func(t *testing.T) {
			dataDir := t.TempDir()
			repo, stale, _ := staleTicket(t, dataDir)
			useFakeAgent(t, dataDir, "stop end_turn")

			// Command failure must not undo reconciliation of the
			// stale ticket.
			runIn(t, dataDir, repo, args...)

			if got := testfix.ReadTicket(t, dataDir, stale); got.Status != store.Failed {
				t.Errorf("ticket %d is %q, want %q", stale, got.Status, store.Failed)
			}
		})
	}
}

// Finishing can succeed after reconciliation has marked the stale run failed.
func TestFinishCanCompleteTheTicketTheReconcileJustFailed(t *testing.T) {
	dataDir := t.TempDir()
	repo, stale, _ := staleTicket(t, dataDir)
	s := testfix.OpenStore(t, dataDir)
	ticket, err := s.Ticket(stale)
	if err != nil {
		t.Fatal(err)
	}
	commit := testfix.GitOut(t, repo, "rev-parse", ticket.Branch)

	_, err = runIn(t, dataDir, repo, "finish", fmt.Sprint(stale), commit)
	if err != nil {
		t.Fatal(err)
	}

	if got := testfix.ReadTicket(t, dataDir, stale); got.Status != store.Ready {
		t.Errorf("ticket %d is %q, want %q", stale, got.Status, store.Ready)
	}
}

// The person types an ordinary command and the conflict goes back to the agent
// that wrote the work. Today they have to notice it and open a chat to ask for
// the merge.
func TestACommandStartsTheRunThatCorrectsAConflict(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo := readyTicket(t, dataDir)
	commitReadyWork(t, s, dataDir, ticketID)
	landConflictingChange(t, repo)
	sent := filepath.Join(t.TempDir(), "prompt")
	useFakeAgent(t, dataDir, "prompt "+sent, "stop end_turn")
	useConflictLaunch(t, func(id int64) *exec.Cmd {
		return exec.Command(testfix.DG(t), "ticket", "run", "--conflict", fmt.Sprint(id))
	})
	reported, err := s.Run(ticketID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := runIn(t, dataDir, repo, "list"); err != nil {
		t.Fatal(err)
	}

	waitForCompletedRun(t, s, ticketID, reported.ID)
	got := testfix.WaitFor(t, sent)
	want := fmt.Sprintf("no longer merges into %s", repoBranch)
	if !strings.Contains(got, want) {
		t.Errorf("the agent of the conflict run received:\n%s\nwant it to hold %q", got, want)
	}
}

// Use a live PID with an expired configured timeout to prove reconciliation
// reads the settings snapshot.
func TestTheReconcileTakesTheTimeoutFromTheConfig(t *testing.T) {
	dataDir := t.TempDir()
	_, stale, repo, _ := runningTicket(t, dataDir)
	if err := testfix.OpenStore(t, dataDir).SetSetting("timeout_minutes", "1"); err != nil {
		t.Fatal(err)
	}
	testfix.AgeRun(t, dataDir, stale, 2*time.Minute)

	if _, err := runIn(t, dataDir, repo); err != nil {
		t.Fatal(err)
	}

	if got := testfix.ReadTicket(t, dataDir, stale); got.Status != store.Failed {
		t.Errorf("ticket %d is %q, want %q", stale, got.Status, store.Failed)
	}
}
