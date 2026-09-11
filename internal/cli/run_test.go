package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/alcubie/delegator/internal/adapters"
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
	launch = l
	t.Cleanup(func() { launch = saved })
}

// fakeAgent returns an Adapter that runs the given script. It is here and not
// in testfix because testfix cannot import adapters.
func fakeAgent(t *testing.T, lines ...string) adapters.Fake {
	t.Helper()
	return adapters.Fake{Binary: testfix.FakeAgentPath, Script: testfix.Script(t, lines...)}
}

// useAgent puts a in place of the agent dg run starts, for one test.
func useAgent(t *testing.T, a adapters.Adapter) {
	t.Helper()
	saved := agent
	agent = a
	t.Cleanup(func() { agent = saved })
}

// The file the script writes has a relative path, so it lands in the worktree
// only if the agent was started there, and it is there only if dg run waited.
func TestRunStartsTheAgentOnTheTicket(t *testing.T) {
	dataDir := t.TempDir()
	s, ticketID, repo := queuedTicket(t, dataDir)
	useAgent(t, fakeAgent(t, "write made-by-the-agent done", "exit 0"))

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
	made := filepath.Join(run.WorktreePath(dataDir, ticketID), "made-by-the-agent")
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
	useAgent(t, fakeAgent(t, "write made-by-the-agent done", "exit 0"))

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
	made := filepath.Join(run.WorktreePath(dataDir, ticketID), "made-by-the-agent")
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
	for _, c := range Root(t.TempDir(), t.TempDir()).Commands() {
		if c.Name() == "run" {
			if !c.Hidden {
				t.Error("dg run is in the help")
			}
			return
		}
	}
	t.Error("dg run is not in the command tree")
}

// A run that ends in ready holds the queue. The supervisor calls Next when its
// run ends, and Next finds the ticket in ready, which is work the person has
// not examined yet, so the second ticket waits until dg accept closes the
// first.
func TestRunStartsNothingWhenItEndsInReady(t *testing.T) {
	dataDir := testfix.XDGDataDir(t)
	_, first, repo := queuedTicket(t, dataDir)
	testfix.SecondTicket(t, dataDir)
	useAgent(t, fakeAgent(t, fmt.Sprintf("run dg finish %d abc123", first), "exit 0"))
	l, marker := testfix.RecordingLaunch(t)
	useLaunch(t, l)

	if _, err := runIn(t, dataDir, repo, "run", fmt.Sprint(first)); err != nil {
		t.Fatal(err)
	}

	testfix.WaitForStarts(t, marker, 0)
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
