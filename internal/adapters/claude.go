package adapters

import (
	"encoding/json"
	"os/exec"
)

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

// Resume returns the argv that reopens a session for a person. It is
// interactive, so no -p and no permission mode: the person is there to answer.
func (c Claude) Resume(session string) []string {
	return []string{claudeBinary, "--resume", session}
}

// SessionID reads session_id from the JSON result that a headless run writes.
// Output that is not that JSON, or JSON with no id in it, gives no id and no
// error: a run that died early is reported by its exit status, not here.
func (c Claude) SessionID(out []byte) (string, error) {
	var result struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		// Not the result object means the run died before writing one. The
		// exit status already reports that; an error here would say it twice.
		return "", nil
	}
	return result.SessionID, nil
}

// Name returns the agent's name.
func (c Claude) Name() string { return claudeBinary }
