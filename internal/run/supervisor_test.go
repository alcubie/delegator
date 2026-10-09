package run

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

const testAgentID int64 = 1

func TestMain(m *testing.M) {
	code := m.Run()
	testfix.CleanupBinaries()
	os.Exit(code)
}

func queuedTicket(t *testing.T, title string) (string, int64) {
	t.Helper()
	repo := repoOnMain(t)
	dataDir := t.TempDir()
	s := testfix.OpenStore(t, dataDir)
	projectID, err := s.ProjectID(repo, "main")
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.AddTicket(projectID, title)
	if err != nil {
		t.Fatal(err)
	}
	testfix.GitIn(t, repo, "checkout", "-q", "-b", "other")
	testfix.CommitIn(t, repo, "second")
	return dataDir, id
}

// Timeout must close the run before signalling its group, which includes the
// supervisor itself, and must terminate both supervisor and child.
func TestTheSupervisorTimeoutStopsTheRunGroupAndFailsTheRun(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	s := testfix.OpenStore(t, dataDir)
	runID, err := s.Claim(id, branch(id, "Add the thing"), testAgentID)
	if err != nil {
		t.Fatal(err)
	}
	pgid := testfix.Group(t, `sleep 60 & : > "$1"; sleep 60`)

	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	timer := startSupervisorTimer(s, runID, pgid, 20*time.Millisecond, cancel)
	if err := <-timer.done; err != nil {
		t.Fatal(err)
	}
	testfix.WaitForGroupGone(t, pgid)

	if got := testfix.ReadTicket(t, dataDir, id).Status; got != store.Failed {
		t.Errorf("status = %q, want %q", got, store.Failed)
	}
	run, err := s.Run(id)
	if err != nil {
		t.Fatal(err)
	}
	if run.EndedAt.IsZero() {
		t.Error("the timed out run has no end time")
	}
}

func TestStartMakesTheWorktreeAndClaimsTheTicket(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	if err := Start(testfix.OpenStore(t, dataDir), id, acpConfig(t, dataDir, "stop end_turn")); err != nil {
		t.Fatal(err)
	}
	ticket := testfix.ReadTicket(t, dataDir, id)
	want := branch(id, "Add the thing")
	if ticket.Branch != want {
		t.Errorf("branch = %q, want %q", ticket.Branch, want)
	}
	worktree := filepath.Join(dataDir, "worktrees", "1")
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("the worktree is not there: %v", err)
	}
	if got := testfix.GitOut(t, worktree, "rev-parse", "--abbrev-ref", "HEAD"); got != want {
		t.Errorf("the worktree is on %q, want %q", got, want)
	}
	if got, base := testfix.GitOut(t, worktree, "rev-parse", "HEAD"), testfix.GitOut(t, ticket.Project.Path, "rev-parse", "main"); got != base {
		t.Errorf("the worktree starts at %s, want main at %s", got, base)
	}
}

func TestStartGivesOneTicketToOneRun(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	cfg := acpConfig(t, dataDir, "stop end_turn")
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		s := testfix.OpenStore(t, dataDir)
		wg.Add(1)
		go func() { defer wg.Done(); errs <- Start(s, id, cfg) }()
	}
	wg.Wait()
	close(errs)
	won := 0
	for err := range errs {
		switch {
		case err == nil:
			won++
		case !errors.Is(err, store.ErrInvalidTicketStateChange):
			t.Errorf("err = %v, want ErrInvalidTicketStateChange", err)
		}
	}
	if won != 1 {
		t.Errorf("%d supervisors took the ticket, want 1", won)
	}
	if got := testfix.ReadTicket(t, dataDir, id); got.Status != store.Failed {
		t.Errorf("status = %q, want %q", got.Status, store.Failed)
	}
}

func TestStartNextTakesTheFirstTicketOfTheQueueAndRunsIt(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	second := testfix.SecondTicket(t, dataDir)
	claimed, err := StartNext(testfix.OpenStore(t, dataDir), acpConfig(t, dataDir, "stop end_turn"))
	if err != nil {
		t.Fatal(err)
	}
	if !claimed {
		t.Error("claimed = false, want true from a supervisor that took a ticket")
	}
	ticket := testfix.ReadTicket(t, dataDir, id)
	if ticket.Status != store.Failed {
		t.Errorf("status = %q, want %q", ticket.Status, store.Failed)
	}
	if ticket.Branch != branch(id, "Add the thing") {
		t.Errorf("branch = %q, want %q", ticket.Branch, branch(id, "Add the thing"))
	}
	if ticket.Session != "fake-1" {
		t.Errorf("session = %q, want %q", ticket.Session, "fake-1")
	}
	if got := testfix.ReadTicket(t, dataDir, second); got.Status != store.Queued || got.Session != "" {
		t.Errorf("the second ticket is %q with session %q, want %q and none", got.Status, got.Session, store.Queued)
	}
}

func TestStartNextWithNothingToClaimStopsWithNoError(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	s := testfix.OpenStore(t, dataDir)
	if _, err := s.Claim(id, branch(id, "Add the thing"), testAgentID); err != nil {
		t.Fatal(err)
	}
	claimed, err := StartNext(s, acpConfig(t, dataDir, "stop end_turn"))
	if err != nil {
		t.Fatalf("err = %v, want nil from a supervisor with nothing to claim", err)
	}
	if claimed {
		t.Error("claimed = true, want false from a supervisor with nothing to claim")
	}
	if _, err := os.Stat(WorktreePath(dataDir, id)); err == nil {
		t.Error("a supervisor with nothing to claim made a worktree")
	}
}

func TestStartRefusesATicketThatIsAlreadyRunning(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	s := testfix.OpenStore(t, dataDir)
	if _, err := s.Claim(id, branch(id, "Add the thing"), testAgentID); err != nil {
		t.Fatal(err)
	}
	err := Start(s, id, acpConfig(t, dataDir, "stop end_turn"))
	if !errors.Is(err, store.ErrInvalidTicketStateChange) {
		t.Fatalf("err = %v, want ErrInvalidTicketStateChange", err)
	}
	held, err := s.Run(id)
	if err != nil {
		t.Fatal(err)
	}
	if !held.EndedAt.IsZero() {
		t.Errorf("the refused start ended the run of the supervisor that holds the ticket at %v", held.EndedAt)
	}
	if _, err := os.Stat(WorktreePath(dataDir, id)); err == nil {
		t.Error("the refused start made a worktree")
	}
}

func TestStartThatCannotMakeTheWorktreeLeavesTheTicketFailed(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	if err := os.RemoveAll(testfix.ReadTicket(t, dataDir, id).Project.Path); err != nil {
		t.Fatal(err)
	}
	s := testfix.OpenStore(t, dataDir)
	if err := Start(s, id, acpConfig(t, dataDir, "stop end_turn")); err == nil {
		t.Fatal("err = nil, want the failure to make the worktree")
	}
	if got := testfix.ReadTicket(t, dataDir, id).Status; got != store.Failed {
		t.Errorf("status = %q, want %q", got, store.Failed)
	}
	failed, err := s.Run(id)
	if err != nil {
		t.Fatal(err)
	}
	if failed.EndedAt.IsZero() {
		t.Error("the run of a ticket that never started has no end time")
	}
	if failed.Agent != "fake" {
		t.Errorf("agent = %q, want %q even though setup failed", failed.Agent, "fake")
	}
}

func logOf(t *testing.T, dataDir string, id int64) string {
	t.Helper()
	logs, err := filepath.Glob(filepath.Join(dataDir, "runs", strconv.FormatInt(id, 10), "*.log"))
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 {
		t.Fatalf("logs = %v, want one", logs)
	}
	data, err := os.ReadFile(logs[0])
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestPromptNamesBothCommands(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "selected instance")
	cacheDir := filepath.Join(dataDir, "cache", "projects", "7")
	got := prompt(42, dataDir, cacheDir)
	for _, want := range []string{"dg ticket show 42", "dg ticket finish 42"} {
		if !strings.Contains(got, want) {
			t.Errorf("the prompt does not hold %q:\n%s", want, got)
		}
	}
	if got, want := strings.Count(got, "--data-dir "+strconv.Quote(dataDir)), 2; got != want {
		t.Errorf("the prompt names the selected data directory %d times, want %d:\n%s", got, want, prompt(42, dataDir, cacheDir))
	}
}

func TestPromptSaysHowToRead(t *testing.T) {
	got := prompt(42, t.TempDir(), t.TempDir())
	for _, want := range []string{"grep", "200 lines", "100 lines"} {
		if !strings.Contains(got, want) {
			t.Errorf("the prompt does not hold %q:\n%s", want, got)
		}
	}
}

func TestPromptExplainsTheSharedDisposableProjectCache(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "cache", "projects", "7")
	got := prompt(42, t.TempDir(), cacheDir)
	for _, want := range []string{
		cacheDir,
		"shared by concurrent tickets",
		"caches that support concurrent access",
		"ticket-specific temporary artifacts in this worktree",
		"disposable",
		"not removed with this worktree",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the prompt does not hold %q:\n%s", want, got)
		}
	}
}

func TestStartKeepsTheLogOfEachRunOfATicket(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	s := testfix.OpenStore(t, dataDir)
	cfg := acpConfig(t, dataDir, "text a line", "stop end_turn")
	if err := Start(s, id, cfg); err != nil {
		t.Fatal(err)
	}
	if err := Restart(s, id, cfg, ""); err != nil {
		t.Fatal(err)
	}
	logs, err := filepath.Glob(filepath.Join(dataDir, "runs", strconv.FormatInt(id, 10), "*.log"))
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 {
		t.Errorf("logs = %v, want one for each run", logs)
	}
}
