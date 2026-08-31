package adapters

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// fakeAgentBinary builds cmd/dg-fake-agent and returns its path. Section 10.2
// asks for a real program and not a function of a test, so the test builds one.
func fakeAgentBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dg-fake-agent")
	out, err := exec.Command("go", "build", "-o", path,
		"github.com/alcubie/delegator/cmd/dg-fake-agent").CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v: %s", err, out)
	}
	return path
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
	fake := Fake{Binary: fakeAgentBinary(t), Script: scriptFile(t, "write made-here the work\nexit 0\n")}

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
	fake := Fake{Binary: fakeAgentBinary(t), Script: scriptFile(t, "exit 3\n")}

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
