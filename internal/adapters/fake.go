package adapters

import "os/exec"

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
// worktree. The session needs no argument: SessionID hands it straight back,
// because the fake agent keeps no session of its own.
func (f Fake) Launch(spec RunSpec) *exec.Cmd {
	cmd := exec.Command(f.binary(), f.Script)
	cmd.Dir = spec.Worktree
	return cmd
}

// Resume returns the argv that replays the script. The fake agent keeps no
// conversation, so there is nothing else to go back to.
func (f Fake) Resume(session string) []string {
	return []string{f.binary(), f.Script}
}

// SessionID returns the id delegator gave the run. The fake agent stands in for
// claude, which accepts a supplied id, so it never parses out.
func (f Fake) SessionID(spec RunSpec, out []byte) (string, error) {
	return spec.Session, nil
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
