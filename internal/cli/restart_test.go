package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/run"
	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

// restartIn runs dg restart on one ticket and stops the test if the command
// gives an error. The command writes nothing: the person named the ticket, so
// there is nothing to tell them that they do not know.
func restartIn(t *testing.T, dataDir, workDir string, id int64) {
	t.Helper()
	out, err := runIn(t, dataDir, workDir, "restart", fmt.Sprint(id))
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("dg restart wrote %q, want nothing", out)
	}
}

// Only a failed ticket restarts. A queued ticket keeps its place, which a
// restart that went through would have made it run again.
func TestRestartRefusesATicketThatDidNotFail(t *testing.T) {
	tests := []struct {
		state string
		setUp func(t *testing.T, dataDir string) (*store.Store, int64, string)
	}{
		{"queued", queuedTicket},
		{"ready", readyTicket},
	}
	for _, test := range tests {
		t.Run(test.state, func(t *testing.T) {
			dataDir := t.TempDir()
			_, ticketID, repo := test.setUp(t, dataDir)
			testfix.SecondTicket(t, dataDir)
			before := testfix.ReadTicket(t, dataDir, ticketID)

			_, err := runIn(t, dataDir, repo, "restart", fmt.Sprint(ticketID))

			if !errors.Is(err, store.ErrInvalidTicketStateChange) {
				t.Fatalf("err = %v, want ErrInvalidTicketStateChange", err)
			}
			if got := testfix.ReadTicket(t, dataDir, ticketID); got.Status != before.Status || got.Position != before.Position {
				t.Errorf("the ticket is %q at %d, want %q at %d",
					got.Status, got.Position, before.Status, before.Position)
			}
		})
	}
}

// A restart starts the supervisor for the failed ticket directly rather than
// putting the ticket back in the queue to wait behind other work.
func TestRestartStartsTheRun(t *testing.T) {
	dataDir := t.TempDir()
	_, ticketID, repo := failedTicket(t, dataDir)
	l, marker := testfix.RecordingLaunch(t)
	useLaunch(t, l)
	var launched int64
	saved := restartLaunch
	restartLaunch = func(id int64) *exec.Cmd {
		launched = id
		return l()
	}
	t.Cleanup(func() { restartLaunch = saved })

	restartIn(t, dataDir, repo, ticketID)

	if launched != ticketID {
		t.Errorf("restart launched ticket %d, want %d", launched, ticketID)
	}
	testfix.WaitForStarts(t, marker, 1)
}

// The run that follows a restart works in the worktree of the run that
// failed, so the agent keeps the files that run left there and no commit
// holds.
func TestRestartRunsTheAgentInTheWorktreeOfTheFailedRun(t *testing.T) {
	dataDir := testfix.XDGDataDir(t)
	_, ticketID, repo := queuedTicket(t, dataDir)
	useAgent(t, fakeAgent(t, "write made-by-the-agent done", "exit 0"))
	if _, err := runIn(t, dataDir, repo, "run", fmt.Sprint(ticketID)); err != nil {
		t.Fatal(err)
	}
	worktree := run.WorktreePath(dataDir, ticketID)
	if err := os.WriteFile(filepath.Join(worktree, "not-committed"), []byte("work"), 0o600); err != nil {
		t.Fatal(err)
	}

	restartIn(t, dataDir, repo, ticketID)
	if _, err := runIn(t, dataDir, repo, "run", fmt.Sprint(ticketID)); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(worktree, "not-committed")); err != nil {
		t.Errorf("the second run did not get the worktree of the first: %v", err)
	}
	if _, err := os.Stat(filepath.Join(worktree, "made-by-the-agent")); err != nil {
		t.Errorf("the agent did not run in the worktree of the first run: %v", err)
	}
}

// The second claim writes a row of its own, so the ticket has a run for each
// time it was given to an agent and dg show can give the history of both.
func TestRestartGivesTheTicketASecondRun(t *testing.T) {
	dataDir := testfix.XDGDataDir(t)
	s, ticketID, repo := queuedTicket(t, dataDir)
	useAgent(t, fakeAgent(t, "exit 0"))
	if _, err := runIn(t, dataDir, repo, "run", fmt.Sprint(ticketID)); err != nil {
		t.Fatal(err)
	}
	first, err := s.Run(ticketID)
	if err != nil {
		t.Fatal(err)
	}

	restartIn(t, dataDir, repo, ticketID)
	if _, err := runIn(t, dataDir, repo, "run", fmt.Sprint(ticketID)); err != nil {
		t.Fatal(err)
	}

	second, err := s.Run(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID {
		t.Error("the second run wrote the row of the first")
	}
}

// A restart is not something a person types by hand for a ticket they have
// not read, so dg restart takes the id of the ticket and nothing else.
func TestRestartWithNoID(t *testing.T) {
	if _, err := runIn(t, t.TempDir(), t.TempDir(), "restart"); err == nil {
		t.Error("err = nil, want dg restart to ask for an id")
	}
}

// A word that is not a number is not the id of a ticket, and the command says
// so rather than acting on a ticket the person did not name.
func TestRestartWithAnIDThatIsNotANumber(t *testing.T) {
	_, err := runIn(t, t.TempDir(), t.TempDir(), "restart", "seven")
	if err == nil || !strings.Contains(err.Error(), "seven") {
		t.Errorf("err = %v, want it to name seven", err)
	}
}
