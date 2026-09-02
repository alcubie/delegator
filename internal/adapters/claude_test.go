package adapters

import (
	"slices"
	"testing"
)

func TestClaudeLaunchGivesAHeadlessRun(t *testing.T) {
	cmd := Claude{}.Launch(RunSpec{Worktree: "/w", Prompt: "do the thing"})

	want := []string{
		"claude", "-p", "do the thing",
		"--output-format", "json",
		"--permission-mode", "bypassPermissions",
	}
	if !slices.Equal(cmd.Args, want) {
		t.Errorf("argv = %q, want %q", cmd.Args, want)
	}
	if cmd.Dir != "/w" {
		t.Errorf("dir = %q, want %q", cmd.Dir, "/w")
	}
}

var _ Adapter = Claude{}

// A person continues the conversation, so the run is interactive: no -p, and
// no permission mode, because the person is there to answer.
func TestClaudeResumeContinuesTheSession(t *testing.T) {
	const session = "e55e382e-2c88-4de7-a31d-ab8763a0fb5a"

	got := Claude{}.Resume(session)

	want := []string{"claude", "--resume", session}
	if !slices.Equal(got, want) {
		t.Errorf("argv = %q, want %q", got, want)
	}
}
