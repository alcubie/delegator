package adapters

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/alcubie/delegator/internal/agentbin"
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

// scriptFile writes one script and returns its path.
func scriptFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "script")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

var _ Adapter = Fake{}

// The path in the script is relative, so the file lands in the worktree only if
// Launch gave the command that directory to work in.
func TestFakeLaunchStartsTheFakeAgentWithTheScript(t *testing.T) {
	worktree := t.TempDir()
	fake := Fake{Binary: fakeAgentPath, Script: scriptFile(t, "write made-here the work\nexit 0\n")}

	if err := fake.Launch(RunSpec{Worktree: worktree, Session: "s-1"}).Run(); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(filepath.Join(worktree, "made-here"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(content); got != "the work" {
		t.Errorf("content = %q, want %q", got, "the work")
	}
}

// The supervisor decides that a run failed from the status of the command, so
// the status of the script must reach it.
func TestFakeLaunchGivesTheStatusOfTheScript(t *testing.T) {
	fake := Fake{Binary: fakeAgentPath, Script: scriptFile(t, "exit 3\n")}

	err := fake.Launch(RunSpec{Worktree: t.TempDir()}).Run()

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("err = %v, want an ExitError", err)
	}
	if got := exitErr.ExitCode(); got != 3 {
		t.Errorf("status = %d, want 3", got)
	}
}

func TestFakeSessionIDGivesBackTheSessionItWasGiven(t *testing.T) {
	got, err := Fake{}.SessionID(RunSpec{Session: "s-1"}, []byte("output that names no session"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "s-1" {
		t.Errorf("session = %q, want %q", got, "s-1")
	}
}
