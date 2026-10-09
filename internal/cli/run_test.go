package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/run"
	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

func TestMain(m *testing.M) {
	// Disable real launches: under go test the current executable is the
	// test binary, which would recursively spawn this suite.
	launch = func() *exec.Cmd { return exec.Command("true") }
	launchTelemetry = func(string) *exec.Cmd { return exec.Command("true") }
	sendTelemetry = func(*store.Store, time.Time, string) error { return nil }
	// Disable interactive agent launches; tests install observable
	// substitutes.
	chat = func([]string, string) *exec.Cmd { return exec.Command("true") }
	// Clear inherited color settings; tests that need them set their own
	// values.
	os.Unsetenv("NO_COLOR")
	os.Unsetenv("CLICOLOR_FORCE")
	code := m.Run()
	testfix.CleanupBinaries()
	os.Exit(code)
}

// useLaunch replaces the supervisor launcher until test cleanup.
func useLaunch(t *testing.T, l func() *exec.Cmd) {
	t.Helper()
	saved := launch
	savedRestart := restartLaunch
	launch = l
	restartLaunch = func(int64, string) *exec.Cmd { return l() }
	t.Cleanup(func() {
		launch = saved
		restartLaunch = savedRestart
	})
}

func TestSupervisorLaunchCarriesTheSelectedDataDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "selected")
	cmd := launchIn(dir, dgRun())
	if got, want := cmd.Args[len(cmd.Args)-2:], []string{"--data-dir", dir}; !slices.Equal(got, want) {
		t.Errorf("supervisor arguments end in %v, want %v", got, want)
	}
}

// waitForCompletedRun waits for a detached supervisor to add and end a run.
// after is the id of an earlier run when the test is waiting for a restart.
func waitForCompletedRun(t *testing.T, s *store.Store, id, after int64) store.Run {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		held, err := s.Run(id)
		if err == nil && held.ID > after && !held.EndedAt.IsZero() {
			return held
		}
		if err != nil && !errors.Is(err, store.ErrNoRun) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	ticket, err := s.Ticket(id)
	t.Fatalf("the detached run after %d did not end; ticket = %+v, err = %v", after, ticket, err)
	return store.Run{}
}

func assertDefaultDataDirUnused(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the default data directory was used: %v", err)
	}
}

// The command a person types returns after detaching its supervisor. The
// supervisor is a new dg process, so this covers the argv boundary at which an
// explicit instance used to be lost to the platform default.
func TestDetachedSupervisorKeepsTheSelectedDataDirectory(t *testing.T) {
	defaultDir := testfix.XDGDataDir(t)
	dataDir := filepath.Join(t.TempDir(), "selected")
	repo := testfix.Repo(t, repoBranch)
	testfix.CommitIn(t, repo, "first")
	promptPath := filepath.Join(t.TempDir(), "prompt")
	useFakeAgent(t, dataDir, "prompt "+promptPath, "stop end_turn")
	useLaunch(t, func() *exec.Cmd { return exec.Command(testfix.DG(t), "run") })

	out, err := runIn(t, dataDir, repo, "ticket", "create", "Run in the selected instance", "--no-body")
	if err != nil {
		t.Fatal(err)
	}
	id := idOf(t, out)
	s := testfix.OpenStore(t, dataDir)
	waitForCompletedRun(t, s, id, 0)
	if got := testfix.ReadTicket(t, dataDir, id).Status; got != store.Failed {
		t.Errorf("status = %q, want %q", got, store.Failed)
	}
	prompt, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		fmt.Sprintf("dg ticket show %d --data-dir %q", id, dataDir),
		fmt.Sprintf("dg ticket finish %d <hash> --data-dir %q", id, dataDir),
	} {
		if !strings.Contains(string(prompt), want) {
			t.Errorf("the agent prompt does not hold %q:\n%s", want, prompt)
		}
	}
	assertDefaultDataDirUnused(t, defaultDir)
}

// useFakeAgent makes the ACP fake the default registry agent for one test.
func useFakeAgent(t *testing.T, dataDir string, lines ...string) {
	t.Helper()
	testfix.UseAgent(t, dataDir, "fake", testfix.FakeAgent(t), testfix.Script(t, lines...))
}

// The worktree marker proves the agent ran in that directory and was awaited.
func TestRunStartsTheAgentOnTheTicket(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo := queuedTicket(t, dataDir)
	made := filepath.Join(run.WorktreePath(dataDir, ticketID), "made-by-the-agent")
	useFakeAgent(t, dataDir, "write "+made+" done", "stop end_turn")

	out, err := runIn(t, dataDir, repo, "run", fmt.Sprint(ticketID))
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("dg run wrote %q, want nothing", out)
	}

	ticket, err := s.Ticket(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	// The agent of this test does not call dg finish, so the run gave no
	// report and the supervisor failed the ticket as it stopped.
	if ticket.Status != store.Failed {
		t.Errorf("status = %q, want %q", ticket.Status, store.Failed)
	}
	if _, err := os.Stat(made); err != nil {
		t.Errorf("the agent did not run in the worktree: %v", err)
	}
}

// Automatic run commands leave atomic ticket selection to the supervisor.
func TestRunWithNoIDStartsTheFirstTicketOfTheQueue(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo := queuedTicket(t, dataDir)
	second := testfix.SecondTicket(t, dataDir)
	made := filepath.Join(run.WorktreePath(dataDir, ticketID), "made-by-the-agent")
	useFakeAgent(t, dataDir, "write "+made+" done", "stop end_turn")

	if _, err := runIn(t, dataDir, repo, "run"); err != nil {
		t.Fatal(err)
	}

	ticket, err := s.Ticket(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	// The agent of this test does not call dg finish, so the run gave no
	// report and the supervisor failed the ticket as it stopped.
	if ticket.Status != store.Failed {
		t.Errorf("status = %q, want %q", ticket.Status, store.Failed)
	}
	if _, err := os.Stat(made); err != nil {
		t.Errorf("the agent did not run in the worktree of the first ticket: %v", err)
	}
	held, err := s.Run(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if held.PID != os.Getpid() {
		t.Errorf("the run holds pid %d, want the supervisor's %d", held.PID, os.Getpid())
	}
	if got, err := s.Ticket(second); err != nil || got.Status != store.Queued {
		t.Errorf("the second ticket is %q (err %v), want %q", got.Status, err, store.Queued)
	}
}

// Keep the supervisor entry point hidden from ordinary help but callable
// directly.
func TestRunIsHiddenFromTheHelp(t *testing.T) {
	for _, c := range Root(t.TempDir()).Commands() {
		if c.Name() == "run" {
			if !c.Hidden {
				t.Error("dg run is in the help")
			}
			return
		}
	}
	t.Error("dg run is not in the command tree")
}

// A blocked ticket with spare capacity must not trigger another supervisor.
// Repeated empty claims previously caused an endless launch chain.
func TestRunWithNoIDThatClaimsNothingStartsNothing(t *testing.T) {
	dataDir := testfix.XDGDataDir(t)
	s, first, repo := queuedTicket(t, dataDir)
	second := testfix.SecondTicket(t, dataDir)
	if err := s.AddDependencies(second, first); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeStatus(first, store.Cancelled); err != nil {
		t.Fatal(err)
	}
	l, record := testfix.RecordingLaunch(t)
	useLaunch(t, l)

	out, err := runIn(t, dataDir, repo, "run")
	if err != nil {
		t.Fatalf("err = %v, want nil: a supervisor with nothing to claim is not a fault", err)
	}
	if out != "" {
		t.Errorf("dg run wrote %q, want nothing", out)
	}

	testfix.WaitForStarts(t, record, 0)
}

// A completed run must trigger more work after freeing its capacity.
func TestRunWithNoIDStartsTheNextWhenItsRunEnds(t *testing.T) {
	dataDir := testfix.XDGDataDir(t)
	_, _, repo := queuedTicket(t, dataDir)
	testfix.SecondTicket(t, dataDir)
	useFakeAgent(t, dataDir, "stop end_turn")
	l, record := testfix.RecordingLaunch(t)
	useLaunch(t, l)

	if _, err := runIn(t, dataDir, repo, "run"); err != nil {
		t.Fatal(err)
	}

	testfix.WaitForStarts(t, record, 1)
}

// The next supervisor starts after the first one has finished, in a process
// with no in-memory selection to inherit. Its argv must name the same instance
// even when the platform default points elsewhere.
func TestNextSupervisorKeepsTheSelectedDataDirectory(t *testing.T) {
	defaultDir := testfix.XDGDataDir(t)
	dataDir := filepath.Join(t.TempDir(), "selected")
	s, _, repo := queuedTicket(t, dataDir)
	second := testfix.SecondTicket(t, dataDir)
	useFakeAgent(t, dataDir, "stop end_turn")
	useLaunch(t, func() *exec.Cmd { return exec.Command(testfix.DG(t), "run") })

	if _, err := runIn(t, dataDir, repo, "run"); err != nil {
		t.Fatal(err)
	}
	waitForCompletedRun(t, s, second, 0)
	if got := testfix.ReadTicket(t, dataDir, second).Status; got != store.Failed {
		t.Errorf("status = %q, want %q", got, store.Failed)
	}
	assertDefaultDataDirUnused(t, defaultDir)
}

// Remove the repository to force setup failure; do not trigger a replacement
// that would repeat the same failure.
func TestRunThatFailsToStartStartsNothing(t *testing.T) {
	dataDir := testfix.XDGDataDir(t)
	_, id, repo := queuedTicket(t, dataDir)
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}
	l, record := testfix.RecordingLaunch(t)
	useLaunch(t, l)

	if _, err := runIn(t, dataDir, t.TempDir(), "run", fmt.Sprint(id)); err == nil {
		t.Fatal("err = nil, want the failure to make the worktree")
	}

	testfix.WaitForStarts(t, record, 0)
}
