package adapters

import (
	"bytes"
	"encoding/json"
	"os/exec"
)

// claudeBinary is the command that Claude starts.
const claudeBinary = "claude"

// Claude is the Adapter for claude, the agent of version 1.
type Claude struct{}

// Launch returns the command for a headless run in the spec's worktree.
//
// The output is one JSON object per line, and the first line already carries
// the session id, so a run that dies part way has still reported it and a
// person can open the session to see what happened. The single-object format
// writes its id last, which is the one line a dying run never reaches.
//
// The permission mode turns off the questions claude would otherwise ask,
// because no person is at the terminal to answer one. The worktree is the
// area that a run can reach, and it is isolation rather than a sandbox.
//
// A spec that carries a session adds --resume, which continues that
// conversation rather than starting one. The run is still headless, so the
// prompt goes with it and the agent reads the ticket again.
func (c Claude) Launch(spec RunSpec) *exec.Cmd {
	args := []string{
		"-p", spec.Prompt,
		"--output-format", "stream-json", "--verbose",
		"--permission-mode", "bypassPermissions",
	}
	if spec.Session != "" {
		args = append(args, "--resume", spec.Session)
	}
	cmd := exec.Command(claudeBinary, args...)
	cmd.Dir = spec.Worktree
	return cmd
}

// Resume returns the argv that reopens a session for a person. It is
// interactive, so no -p and no permission mode: the person is there to answer.
func (c Claude) Resume(session string) []string {
	return []string{claudeBinary, "--resume", session}
}

// SessionID reads session_id from the stream a headless run writes. Every
// line of that stream carries it, so the first line that parses is enough,
// and a run cut off after its first line still yields its id.
//
// Output with no such line gives no id and no error: the run never got as far
// as starting a session, and its exit status already reports that.
func (c Claude) SessionID(out []byte) (string, error) {
	for line := range bytes.SplitSeq(out, []byte("\n")) {
		var event struct {
			SessionID string `json:"session_id"`
		}
		if json.Unmarshal(line, &event) == nil && event.SessionID != "" {
			return event.SessionID, nil
		}
	}
	return "", nil
}

// Name returns the agent's name.
func (c Claude) Name() string { return claudeBinary }
