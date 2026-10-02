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

// restartIn runs dg restart and checks that it succeeds silently.
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

// Restart selects the failed ticket directly, bypassing queued work.
func TestRestartStartsTheRun(t *testing.T) {
	dataDir := t.TempDir()
	_, ticketID, repo := failedTicket(t, dataDir)
	l, record := testfix.RecordingLaunch(t)
	useLaunch(t, l)
	var launched int64
	saved := restartLaunch
	restartLaunch = func(id int64, _ string) *exec.Cmd {
		launched = id
		return l()
	}
	t.Cleanup(func() { restartLaunch = saved })

	restartIn(t, dataDir, repo, ticketID)

	if launched != ticketID {
		t.Errorf("restart launched ticket %d, want %d", launched, ticketID)
	}
	testfix.WaitForStarts(t, record, 1)
}

// A restart is detached just like a new run, but it names the failed ticket
// directly. The new dg process must receive the selected instance as well as
// that id instead of looking in the platform default database.
func TestRestartSupervisorKeepsTheSelectedDataDirectory(t *testing.T) {
	defaultDir := testfix.XDGDataDir(t)
	dataDir := filepath.Join(t.TempDir(), "selected")
	s, ticketID, repo := failedTicket(t, dataDir)
	useFakeAgent(t, dataDir, "stop end_turn")
	before, err := s.Run(ticketID)
	if err != nil {
		t.Fatal(err)
	}

	saved := restartLaunch
	restartLaunch = func(id int64, model string) *exec.Cmd {
		cmd := dgRestart(id, model)
		cmd.Path = testfix.DG(t)
		cmd.Args[0] = cmd.Path
		return cmd
	}
	t.Cleanup(func() { restartLaunch = saved })

	restartIn(t, dataDir, repo, ticketID)
	waitForCompletedRun(t, s, ticketID, before.ID)
	if got := testfix.ReadTicket(t, dataDir, ticketID).Status; got != store.Failed {
		t.Errorf("status = %q, want %q", got, store.Failed)
	}
	assertDefaultDataDirUnused(t, defaultDir)
}

func TestRestartModelReachesDetachedSupervisor(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo := queuedTicket(t, dataDir)
	selected := filepath.Join(t.TempDir(), "selected-model")
	useFakeAgent(t, dataDir, "models: model-v1 model-v2", "stop refusal")
	if _, err := runIn(t, dataDir, repo, "run", fmt.Sprint(ticketID)); err == nil {
		t.Fatal("expected the first run to refuse")
	}
	first, err := s.Run(ticketID)
	if err != nil || first.Model != "model-v1" {
		t.Fatalf("first model = %+v, %v", first, err)
	}
	useFakeAgent(t, dataDir, "models: model-v2", "selected-model "+selected, "stop end_turn")
	saved := restartLaunch
	restartLaunch = func(id int64, model string) *exec.Cmd {
		cmd := dgRestart(id, model)
		cmd.Path = testfix.DG(t)
		cmd.Args[0] = cmd.Path
		return cmd
	}
	t.Cleanup(func() { restartLaunch = saved })

	if _, err := runIn(t, dataDir, repo, "restart", fmt.Sprint(ticketID), "--model", "model-v2"); err != nil {
		t.Fatal(err)
	}
	second := waitForCompletedRun(t, s, ticketID, first.ID)
	if got, err := os.ReadFile(selected); err != nil || string(got) != "model-v2" {
		t.Fatalf("selected model = %q, %v", got, err)
	}
	if second.Model != "model-v2" {
		t.Errorf("recorded model = %q, want model-v2", second.Model)
	}
}

func TestRestartRejectsEmptyModel(t *testing.T) {
	_, err := runIn(t, t.TempDir(), t.TempDir(), "restart", "42", "--model=")
	if err == nil || !strings.Contains(err.Error(), "--model must be a model ID") {
		t.Errorf("error = %v, want a model ID error", err)
	}
}

// Restart must preserve uncommitted files in the existing worktree.
func TestRestartRunsTheAgentInTheWorktreeOfTheFailedRun(t *testing.T) {
	dataDir := testfix.XDGDataDir(t)
	_, ticketID, repo := queuedTicket(t, dataDir)
	worktree := run.WorktreePath(dataDir, ticketID)
	useFakeAgent(t, dataDir, "write "+filepath.Join(worktree, "made-by-the-agent")+" done", "stop end_turn")
	if _, err := runIn(t, dataDir, repo, "run", fmt.Sprint(ticketID)); err != nil {
		t.Fatal(err)
	}
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

// Each restart creates a separate run record.
func TestRestartGivesTheTicketASecondRun(t *testing.T) {
	dataDir := testfix.XDGDataDir(t)
	s, ticketID, repo := queuedTicket(t, dataDir)
	useFakeAgent(t, dataDir, "stop end_turn")
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

func TestRestartWithNoID(t *testing.T) {
	if _, err := runIn(t, t.TempDir(), t.TempDir(), "restart"); err == nil {
		t.Error("err = nil, want dg restart to ask for an id")
	}
}

func TestRestartWithAnIDThatIsNotANumber(t *testing.T) {
	_, err := runIn(t, t.TempDir(), t.TempDir(), "restart", "seven")
	if err == nil || !strings.Contains(err.Error(), "seven") {
		t.Errorf("err = %v, want it to name seven", err)
	}
}
