package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	launch = func(int64) *exec.Cmd { return exec.Command("true") }
	os.Exit(testfix.RunTests(m, true))
}

// useLaunch puts l in place of the launch of dg run, for one test. The tests
// must replace it: the real launch starts this program's own executable, which
// in a test is the test binary.
func useLaunch(t *testing.T, l func(id int64) *exec.Cmd) {
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
	if ticket.Status != store.Running {
		t.Errorf("status = %q, want %q", ticket.Status, store.Running)
	}
	made := filepath.Join(run.WorktreePath(dataDir, ticketID), "made-by-the-agent")
	if _, err := os.Stat(made); err != nil {
		t.Errorf("the agent did not run in the worktree: %v", err)
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

// The queue goes on with no command from a person: the supervisor of a run
// that ended starts the next ticket itself. The fake agent finishes its ticket
// through dg finish, so the ticket leaves running and Next has a free slot.
func TestRunStartsTheNextTicketWhenItEnds(t *testing.T) {
	dataDir := testfix.XDGDataDir(t)
	_, first, repo := queuedTicket(t, dataDir)
	second := testfix.SecondTicket(t, dataDir)
	useAgent(t, fakeAgent(t, fmt.Sprintf("run dg finish %d abc123", first), "exit 0"))
	l, marker := testfix.RecordingLaunch(t)
	useLaunch(t, l)

	if _, err := runIn(t, dataDir, repo, "run", fmt.Sprint(first)); err != nil {
		t.Fatal(err)
	}

	if got := testfix.WaitFor(t, marker); got != fmt.Sprint(second) {
		t.Errorf("started ticket %s, want %d", got, second)
	}
}

// A run that failed before it claimed its ticket leaves that ticket first in
// the queue. If it then started the next ticket, it would start that same
// ticket again, and each failure would start the next failure without end.
// The repository is removed so the worktree cannot be made, which is a
// failure before the claim.
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

	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Errorf("a run was started after a failed start: ticket %s", testfix.WaitFor(t, marker))
	}
}

// A person adds a ticket and walks away, so dg ticket is the command that
// starts the queue moving.
func TestTicketStartsARunWhenNothingIsRunning(t *testing.T) {
	dataDir := testfix.XDGDataDir(t)
	repo := testfix.Repo(t, repoBranch)
	l, marker := testfix.RecordingLaunch(t)
	useLaunch(t, l)

	out, err := runIn(t, dataDir, repo, "ticket", "Add the thing")
	if err != nil {
		t.Fatal(err)
	}

	if got := testfix.WaitFor(t, marker); got != strings.TrimSpace(out) {
		t.Errorf("started ticket %s, want the new ticket %s", got, strings.TrimSpace(out))
	}
}
