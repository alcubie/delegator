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
	"github.com/alcubie/delegator/internal/agentbin"
	"github.com/alcubie/delegator/internal/run"
	"github.com/alcubie/delegator/internal/store"
)

// fakeAgentPath is dg-fake-agent, built once for the whole package.
var fakeAgentPath string

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

// runTests builds dg-fake-agent once for the package and runs its tests. The
// work is here rather than in TestMain so the cleanup can be deferred: os.Exit
// does not run deferred calls, and every path out of TestMain ends in one.
func runTests(m *testing.M) int {
	binary, remove, err := agentbin.Build()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer remove()

	fakeAgentPath = binary

	// The fake agent's scripts call dg finish, and the child dg must be this
	// build of dg, not the one installed. It goes beside the fake agent, in
	// the directory Build made and removes.
	dg := filepath.Join(filepath.Dir(binary), "dg")
	if out, err := exec.Command("go", "build", "-o", dg, "github.com/alcubie/delegator/cmd/dg").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "go build dg: %v: %s\n", err, out)
		return 1
	}
	os.Setenv("PATH", filepath.Dir(dg)+string(os.PathListSeparator)+os.Getenv("PATH"))

	// The real launch starts this program's own executable, and under go test
	// that is the test binary: every dg ticket in these tests would start a
	// copy of the test binary, which would run these tests, which would start
	// more. Tests that care what was launched put their own launch in place.
	launch = func(int64) *exec.Cmd { return exec.Command("true") }

	return m.Run()
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

// recordingLaunch returns a launch that writes the id it was given to a file,
// and the path of that file.
func recordingLaunch(t *testing.T) (func(id int64) *exec.Cmd, string) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "started")
	return func(id int64) *exec.Cmd {
		return exec.Command("sh", "-c", `echo "$1" > "$2"`, "--", fmt.Sprint(id), marker)
	}, marker
}

// waitFor returns the content of path once it exists, or fails the test after
// a short wait. A launched program is not waited on, so the test has to.
func waitFor(t *testing.T, path string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil {
			return strings.TrimSpace(string(data))
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s was not written", path)
	return ""
}

// xdgDataDir points XDG_DATA_HOME at a fresh directory for one test and
// returns the data directory dg will use below it, so that a dg the fake agent
// runs reaches this test's database and not the person's.
func xdgDataDir(t *testing.T) string {
	t.Helper()
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	return filepath.Join(xdg, "delegator")
}

// fakeAgent returns an Adapter that runs the given script.
func fakeAgent(t *testing.T, lines ...string) adapters.Fake {
	t.Helper()
	script := filepath.Join(t.TempDir(), "script")
	if err := os.WriteFile(script, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return adapters.Fake{Binary: fakeAgentPath, Script: script}
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
	dataDir := xdgDataDir(t)
	_, first, repo := queuedTicket(t, dataDir)
	second := secondTicket(t, dataDir)
	useAgent(t, fakeAgent(t, fmt.Sprintf("run dg finish %d abc123", first), "exit 0"))
	l, marker := recordingLaunch(t)
	useLaunch(t, l)

	if _, err := runIn(t, dataDir, repo, "run", fmt.Sprint(first)); err != nil {
		t.Fatal(err)
	}

	if got := waitFor(t, marker); got != fmt.Sprint(second) {
		t.Errorf("started ticket %s, want %d", got, second)
	}
}

// A run that failed before it claimed its ticket leaves that ticket first in
// the queue. If it then started the next ticket, it would start that same
// ticket again, and each failure would start the next failure without end.
// The repository is removed so the worktree cannot be made, which is a
// failure before the claim.
func TestRunThatFailsToStartStartsNothing(t *testing.T) {
	dataDir := xdgDataDir(t)
	_, id, repo := queuedTicket(t, dataDir)
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}
	l, marker := recordingLaunch(t)
	useLaunch(t, l)

	if _, err := runIn(t, dataDir, t.TempDir(), "run", fmt.Sprint(id)); err == nil {
		t.Fatal("err = nil, want the failure to make the worktree")
	}

	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Errorf("a run was started after a failed start: ticket %s", waitFor(t, marker))
	}
}

// A person adds a ticket and walks away, so dg ticket is the command that
// starts the queue moving.
func TestTicketStartsARunWhenNothingIsRunning(t *testing.T) {
	dataDir := xdgDataDir(t)
	repo := gitRepo(t)
	l, marker := recordingLaunch(t)
	useLaunch(t, l)

	out, err := runIn(t, dataDir, repo, "ticket", "Add the thing")
	if err != nil {
		t.Fatal(err)
	}

	if got := waitFor(t, marker); got != strings.TrimSpace(out) {
		t.Errorf("started ticket %s, want the new ticket %s", got, strings.TrimSpace(out))
	}
}
