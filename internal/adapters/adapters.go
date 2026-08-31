// Package adapters is the seam for an agent. Delegator drives claude in
// version 1, and a later version can drive a different agent without changing
// anything above this interface.
//
// The interface stays small deliberately. What delegator knows about a run goes
// down in RunSpec, and the one thing it learns from a run comes back through
// SessionID, so a new agent needs no new field above the seam.
package adapters

import "os/exec"

// RunSpec is everything a headless run of an agent needs.
type RunSpec struct {
	// Worktree is the directory the agent works in.
	Worktree string

	// Session is the id delegator offers the agent. Only claude accepts one,
	// through --session-id. codex, gemini and opencode each mint their own and
	// let you resume by it afterwards, so their adapters ignore this field and
	// report the real id from SessionID.
	Session string

	// Prompt is the first message to the agent.
	Prompt string
}

// Adapter starts one kind of agent.
type Adapter interface {
	// Launch returns the command for a headless run. The caller starts it and
	// waits, so the timeout and the log belong to the caller.
	Launch(spec RunSpec) *exec.Cmd

	// Resume returns the argv for an interactive session. It is argv rather
	// than a built command because a user command in section 9.2 may need to
	// wrap it in a new terminal window.
	Resume(session string) []string

	// SessionID returns the id of the session a finished run used, so it can be
	// resumed later. out is what the run wrote. An agent that accepts a supplied
	// id returns spec.Session and ignores out; an agent that mints its own parses
	// the id out of out.
	SessionID(spec RunSpec, out []byte) (string, error)

	// Name returns the agent's name, for use in messages.
	Name() string
}
