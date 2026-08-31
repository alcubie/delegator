package adapters

import "os/exec"

// defaultBinary is the name that Fake looks for on the PATH when it is given no
// path of its own.
const defaultBinary = "dg-fake-agent"

// Fake is an Adapter that starts dg-fake-agent. The script decides what the run
// does, so a test of the supervisor gets a real program, a real exit status and
// a real call of dg finish, in milliseconds and at no cost.
type Fake struct {
	// Binary is the path of dg-fake-agent. An empty value means the name on
	// the PATH.
	Binary string

	// Script is the path of the script that each run does.
	Script string
}

// Launch returns the command for one run of the fake agent, in the worktree of
// the spec. The session of the spec needs no argument: SessionID returns it
// again, because the fake agent keeps no session of its own.
func (f Fake) Launch(spec RunSpec) *exec.Cmd {
	cmd := exec.Command(f.binary(), f.Script)
	cmd.Dir = spec.Worktree
	return cmd
}

// Resume returns the argv that does the script again. The fake agent holds no
// conversation, so there is nothing else for a person to go back to.
func (f Fake) Resume(session string) []string {
	return []string{f.binary(), f.Script}
}

// SessionID returns the id that delegator gave the run. The fake agent stands
// in for claude, which accepts an id, so it reads nothing from out.
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
