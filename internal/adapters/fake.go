package adapters

import (
	"os/exec"
	"strings"
)

// defaultBinary is the name that Fake looks for on the PATH when it is given no
// path of its own.
const defaultBinary = "dg-fake-agent"

// Fake is an Adapter that starts dg-fake-agent. The script decides what the run
// does, so a supervisor test gets a real process, a real exit status and a real
// dg finish call, in milliseconds and at no cost.
type Fake struct {
	// Binary is the path of dg-fake-agent. An empty value means the name on
	// the PATH.
	Binary string

	// Script is the path of the script that each run does.
	Script string
}

// Launch returns the command for one run of the fake agent, in the spec's
// worktree.
func (f Fake) Launch(spec RunSpec) *exec.Cmd {
	cmd := exec.Command(f.binary(), f.Script)
	cmd.Dir = spec.Worktree
	return cmd
}

// Resume returns the argv that replays the script, with the session on the
// end. The fake agent keeps no conversation, so there is nothing else to go
// back to, and the session is there so that a test of a command that resumes
// can see which session the command asked for.
func (f Fake) Resume(session string) []string {
	return []string{f.binary(), f.Script, session}
}

// sessionPrefix is what a script writes to report a session, standing in for
// the JSON a real agent gives.
const sessionPrefix = "session: "

// SessionID returns the id the script reported, and nothing at all if it
// reported none, which is what a run that died early looks like.
func (f Fake) SessionID(out []byte) (string, error) {
	for line := range strings.SplitSeq(string(out), "\n") {
		if after, ok := strings.CutPrefix(strings.TrimSpace(line), sessionPrefix); ok {
			return after, nil
		}
	}
	return "", nil
}

// Name returns the name of the agent.
func (f Fake) Name() string { return "fake" }

// binary returns the path to start, which is the name on the PATH if the
// caller gave no path.
func (f Fake) binary() string {
	if f.Binary == "" {
		return defaultBinary
	}
	return f.Binary
}
