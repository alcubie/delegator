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
