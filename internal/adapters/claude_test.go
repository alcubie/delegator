package adapters

import (
	"slices"
	"testing"
)

func TestClaudeLaunchGivesAHeadlessRun(t *testing.T) {
	cmd := Claude{}.Launch(RunSpec{Worktree: "/w", Prompt: "do the thing"})

	want := []string{
		"claude", "-p", "do the thing",
		"--output-format", "stream-json", "--verbose",
		"--permission-mode", "bypassPermissions",
	}
	if !slices.Equal(cmd.Args, want) {
		t.Errorf("argv = %q, want %q", cmd.Args, want)
	}
	if cmd.Dir != "/w" {
		t.Errorf("dir = %q, want %q", cmd.Dir, "/w")
	}
}

// A restart continues the conversation of the run that failed, so the
// headless run carries the session the agent reported before. Everything else
// is the same: the run is still headless and still answers no question.
func TestClaudeLaunchContinuesTheSessionOfAnEarlierRun(t *testing.T) {
	const session = "e55e382e-2c88-4de7-a31d-ab8763a0fb5a"

	cmd := Claude{}.Launch(RunSpec{Worktree: "/w", Prompt: "do the thing", Session: session})

	want := []string{
		"claude", "-p", "do the thing",
		"--output-format", "stream-json", "--verbose",
		"--permission-mode", "bypassPermissions",
		"--resume", session,
	}
	if !slices.Equal(cmd.Args, want) {
		t.Errorf("argv = %q, want %q", cmd.Args, want)
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

// The stream reports the id on its first line, before any work, so it is
// there even when the run is cut off before its result.
func TestClaudeSessionIDReadsTheFirstLineOfTheStream(t *testing.T) {
	const session = "e55e382e-2c88-4de7-a31d-ab8763a0fb5a"
	init := `{"type":"system","subtype":"init","session_id":"` + session + `","model":"m"}`

	for name, out := range map[string]string{
		"a whole run": init + "\n" +
			`{"type":"assistant","session_id":"` + session + `"}` + "\n" +
			`{"type":"result","subtype":"success","session_id":"` + session + `"}` + "\n",
		"cut off after init":        init + "\n" + `{"type":"assist`,
		"cut off mid result":        init + "\n" + `{"type":"result","session_id":"e55e38`,
		"only the first line":       init,
		"a warning before init":     "Warning: something\n" + init + "\n",
		"an event with no id first": `{"type":"system","subtype":"startup"}` + "\n" + init + "\n",
	} {
		got, err := Claude{}.SessionID([]byte(out))
		if err != nil {
			t.Errorf("%s: err = %v, want nil", name, err)
		}
		if got != session {
			t.Errorf("%s: session = %q, want %q", name, got, session)
		}
	}
}

// A run that never started a session wrote no id, and that is the state of
// the run rather than an error in reading it: the exit status says it failed.
func TestClaudeSessionIDWithNothingReported(t *testing.T) {
	for _, out := range []string{
		"",
		"panic: it stopped\n",
		`{"type":"system","subtype":"init"}`,
		`{"type":"system","session_id":""}`,
		`{"type":"system","subtype":"init","session_id":"e55e38`,
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
