package adapters

import "os/exec"

// claudeBinary is the command that Claude starts.
const claudeBinary = "claude"

// Claude is the Adapter for claude, the agent of version 1.
type Claude struct{}

// Launch returns the command for a headless run in the spec's worktree.
//
// The JSON output carries the session id, which SessionID reads back. The
// permission mode turns off the questions claude would otherwise ask, because
// no person is at the terminal to answer one. The worktree is the area that a
// run can reach, and it is isolation rather than a sandbox.
func (c Claude) Launch(spec RunSpec) *exec.Cmd {
	cmd := exec.Command(claudeBinary,
		"-p", spec.Prompt,
		"--output-format", "json",
		"--permission-mode", "bypassPermissions",
	)
	cmd.Dir = spec.Worktree
	return cmd
}

func (c Claude) Resume(session string) []string { return nil }

func (c Claude) SessionID(out []byte) (string, error) { return "", nil }

// Name returns the agent's name.
func (c Claude) Name() string { return claudeBinary }
