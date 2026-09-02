package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	return m.Run()
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
