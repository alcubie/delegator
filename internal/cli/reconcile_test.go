package cli

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

// staleTicket makes a data directory that holds a ticket in running whose
// supervisor is gone, and a second ticket in the queue behind it. It returns
// the repository of the project, the stale ticket and the queued one.
func staleTicket(t *testing.T, dataDir string) (repo string, stale, queued int64) {
	t.Helper()
	_, stale, repo, _ = runningTicket(t, dataDir)
	testfix.StaleRun(t, dataDir, stale)
	return repo, stale, testfix.SecondTicket(t, dataDir)
}

// Each command does the reconcile before its own work, so a supervisor that
// stopped with no report is corrected by the next command the person types,
// whichever one that is. A ticket in running holds the queue, so a command
// that left it there would leave delegator with nothing to do and no sign of
// why.
func TestEachCommandDoesTheReconcile(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"list"},
		{"show", "1"},
		{"move", "2", "top"},
		{"ticket", "Add rate limiting", "--no-body"},
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
			useAgent(t, fakeAgent(t, "exit 0"))

			// The command itself can refuse: dg finish and dg accept are
			// given the ticket that the reconcile has just failed. What the
			// test examines is that the reconcile ran either way.
			runIn(t, dataDir, repo, args...)

			if got := testfix.ReadTicket(t, dataDir, stale); got.Status != store.Failed {
				t.Errorf("ticket %d is %q, want %q", stale, got.Status, store.Failed)
			}
		})
	}
}

// The reconcile is before the work of the command and not after it, so the
// command works on the state the reconcile wrote. dg finish on a ticket whose
// supervisor is gone is refused, because by the time it looks the ticket has
// failed.
func TestTheReconcileIsBeforeTheWorkOfTheCommand(t *testing.T) {
	dataDir := t.TempDir()
	repo, stale, _ := staleTicket(t, dataDir)

	_, err := runIn(t, dataDir, repo, "finish", fmt.Sprint(stale), "abc123")

	if !errors.Is(err, store.ErrInvalidTicketStateChange) {
		t.Errorf("err = %v, want ErrInvalidTicketStateChange", err)
	}
}

// How long a run may take is the person's to set, so the reconcile takes the
// timeout from the config file. The supervisor of this run is the test, which
// is alive, and the timeout is the one rule that answers for a run whose
// process id says nothing.
func TestTheReconcileTakesTheTimeoutFromTheConfig(t *testing.T) {
	writeConfig(t, "timeout_minutes = 1\n")
	dataDir := t.TempDir()
	_, stale, repo, _ := runningTicket(t, dataDir)
	testfix.AgeRun(t, dataDir, stale, 2*time.Minute)

	if _, err := runIn(t, dataDir, repo); err != nil {
		t.Fatal(err)
	}

	if got := testfix.ReadTicket(t, dataDir, stale); got.Status != store.Failed {
		t.Errorf("ticket %d is %q, want %q", stale, got.Status, store.Failed)
	}
}
