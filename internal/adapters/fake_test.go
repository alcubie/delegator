package adapters

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/alcubie/delegator/internal/testfix"
)

func TestMain(m *testing.M) {
	os.Exit(testfix.RunTests(m, false))
}

var _ Adapter = Fake{}

// The path in the script is relative, so the file lands in the worktree only if
// Launch gave the command that directory to work in.
func TestFakeLaunchStartsTheFakeAgentWithTheScript(t *testing.T) {
	worktree := t.TempDir()
	fake := Fake{Binary: testfix.FakeAgentPath, Script: testfix.Script(t, "write made-here the work\nexit 0\n")}

	if err := fake.Launch(RunSpec{Worktree: worktree}).Run(); err != nil {
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
	fake := Fake{Binary: testfix.FakeAgentPath, Script: testfix.Script(t, "exit 3\n")}

	err := fake.Launch(RunSpec{Worktree: t.TempDir()}).Run()

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("err = %v, want an ExitError", err)
	}
	if got := exitErr.ExitCode(); got != 3 {
		t.Errorf("status = %d, want 3", got)
	}
}

func TestFakeSessionIDReadsWhatTheRunReported(t *testing.T) {
	out := []byte("doing the work\nsession: s-1\nfinished\n")

	got, err := Fake{}.SessionID(out)
	if err != nil {
		t.Fatal(err)
	}
	if got != "s-1" {
		t.Errorf("session = %q, want %q", got, "s-1")
	}
}

// A run that died before it said anything has no session, and that is not an
// error: the ticket keeps no session and a person cannot resume it.
func TestFakeSessionIDWithNothingReported(t *testing.T) {
	got, err := Fake{}.SessionID([]byte("it stopped early\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("session = %q, want nothing", got)
	}
}

// A command that resumes a session gives the session to the adapter, and a
// test of that command reads it back off the argv.
func TestFakeResumeCarriesTheSession(t *testing.T) {
	fake := Fake{Binary: "/bin/fake", Script: "/tmp/script"}

	got := fake.Resume("s-1")

	want := []string{"/bin/fake", "/tmp/script", "s-1"}
	if !slices.Equal(got, want) {
		t.Errorf("argv = %v, want %v", got, want)
	}
}
