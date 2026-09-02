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

func TestClaudeSessionIDReadsTheJSON(t *testing.T) {
	const session = "e55e382e-2c88-4de7-a31d-ab8763a0fb5a"
	out := []byte(`{"type":"result","session_id":"` + session + `","result":"done"}` + "\n")

	got, err := Claude{}.SessionID(out)
	if err != nil {
		t.Fatal(err)
	}
	if got != session {
		t.Errorf("session = %q, want %q", got, session)
	}
}

// A run that died early wrote no id, and that is the state of the run and not
// an error in reading it: the ticket keeps no session, and the exit status
// says the run failed.
func TestClaudeSessionIDWithNothingReported(t *testing.T) {
	for _, out := range []string{
		"",
		"panic: it stopped\n",
		`{"type":"result","result":"done"}`,
		`{"type":"result","session_id":""}`,
		`{"type":"result","session_id":"e55e38`,
	} {
		got, err := Claude{}.SessionID([]byte(out))
		if err != nil {
			t.Errorf("out %q: err = %v, want nil", out, err)
		}
		if got != "" {
			t.Errorf("out %q: session = %q, want nothing", out, got)
		}
	}
}
