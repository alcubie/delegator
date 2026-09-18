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
	// The real launch starts this program's own executable, and under go test
	// that is the test binary: every dg ticket in these tests would start a
	// copy of the test binary, which would run these tests, which would start
	// more. Tests that care what was launched put their own launch in place.
	launch = func() *exec.Cmd { return exec.Command("true") }
	// The real chat starts the agent of the adapter in a terminal, and no
	// test has a person at one. Tests that care what was started put their
	// own chat in place.
	chat = func([]string, string) *exec.Cmd { return exec.Command("true") }
	// The shell that runs the tests may set either variable, and each test of
	// --color=auto would then see its colour. A test that wants one sets it.
	os.Unsetenv("NO_COLOR")
	os.Unsetenv("CLICOLOR_FORCE")
	os.Exit(testfix.RunTests(m, true))
}

// useLaunch puts l in place of the launch of dg run, for one test. The tests
// must replace it: the real launch starts this program's own executable, which
// in a test is the test binary.
func useLaunch(t *testing.T, l func() *exec.Cmd) {
	t.Helper()
	saved := launch
	savedRestart := restartLaunch
	launch = l
	restartLaunch = func(int64) *exec.Cmd { return l() }
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
	useLaunch(t, func() *exec.Cmd { return exec.Command("dg", "run") })

	out, err := runIn(t, dataDir, repo, "ticket", "Run in the selected instance", "--no-body")
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
		fmt.Sprintf("dg show %d --data-dir %q", id, dataDir),
		fmt.Sprintf("dg finish %d <hash> --data-dir %q", id, dataDir),
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
	testfix.UseAgent(t, dataDir, "fake", testfix.FakeAgentPath, testfix.Script(t, lines...))
}

// The file the script writes is in the worktree, so it is there only if dg run
// started the agent there and waited for it.
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

// dg run with no id is the command a trigger starts. It claims the first
// ticket of the queue for itself, so no trigger has to read the queue and name
// a ticket for it.
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

// A person does not start a run; delegator does. The command stays out of
// dg help so the help lists what a person types, and typing it still works.
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

// A supervisor that claimed nothing launches nothing. The queue here holds a
// ticket and the limit leaves a slot free, so the count of Next says one
// supervisor, but the ticket waits on a cancelled one and no supervisor can
// claim it. Each one that launched another would read the same queue, and the
// chain would not end.
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
	l, marker := testfix.RecordingLaunch(t)
	useLaunch(t, l)

	out, err := runIn(t, dataDir, repo, "run")
	if err != nil {
		t.Fatalf("err = %v, want nil: a supervisor with nothing to claim is not a fault", err)
	}
	if out != "" {
		t.Errorf("dg run wrote %q, want nothing", out)
	}

	testfix.WaitForStarts(t, marker, 0)
}

// A supervisor that claimed a ticket and ran it to its end launches the next
// one. The slot it held is the one that is free now, so the queue has moved
// since the trigger counted the slots.
func TestRunWithNoIDStartsTheNextWhenItsRunEnds(t *testing.T) {
	dataDir := testfix.XDGDataDir(t)
	_, _, repo := queuedTicket(t, dataDir)
	testfix.SecondTicket(t, dataDir)
	useFakeAgent(t, dataDir, "stop end_turn")
	l, marker := testfix.RecordingLaunch(t)
	useLaunch(t, l)

	if _, err := runIn(t, dataDir, repo, "run"); err != nil {
		t.Fatal(err)
	}

	testfix.WaitForStarts(t, marker, 1)
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
	useLaunch(t, func() *exec.Cmd { return exec.Command("dg", "run") })

	if _, err := runIn(t, dataDir, repo, "run"); err != nil {
		t.Fatal(err)
	}
	waitForCompletedRun(t, s, second, 0)
	if got := testfix.ReadTicket(t, dataDir, second).Status; got != store.Failed {
		t.Errorf("status = %q, want %q", got, store.Failed)
	}
	assertDefaultDataDirUnused(t, defaultDir)
}

// A run that could not start leaves the queue where it is. What stopped it is
// the repository of the project or the database, and a run started after it
// would meet the same fault. The repository is removed here, so git cannot
// make the worktree.
func TestRunThatFailsToStartStartsNothing(t *testing.T) {
	dataDir := testfix.XDGDataDir(t)
	_, id, repo := queuedTicket(t, dataDir)
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}
	l, marker := testfix.RecordingLaunch(t)
	useLaunch(t, l)

	if _, err := runIn(t, dataDir, t.TempDir(), "run", fmt.Sprint(id)); err == nil {
		t.Fatal("err = nil, want the failure to make the worktree")
	}

	testfix.WaitForStarts(t, marker, 0)
}
