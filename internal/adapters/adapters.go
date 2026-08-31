// Package adapters holds the seam for an agent. Delegator starts claude in
// version 1, and a later version starts a different agent with no change above
// this interface.
//
// The interface is small on purpose. Everything delegator knows about a run
// goes down in RunSpec, and the one thing it learns from a run comes back
// through SessionID, so an agent that arrives later needs no new field above
// the seam.
package adapters

import "os/exec"

// RunSpec holds everything one headless run of an agent needs.
type RunSpec struct {
	// Worktree is the directory the agent works in.
	Worktree string

	// Session is the id delegator offers the agent. Only claude accepts one,
	// with --session-id. codex, gemini and opencode each make their own and
	// let a caller resume by it afterwards, so an adapter for those ignores
	// this field and reports the real id from SessionID.
	Session string

	// Prompt is the first message to the agent.
	Prompt string
}

// Adapter starts one kind of agent.
type Adapter interface {
	// Launch returns the command for a headless run. The caller starts it and
	// waits, so the timeout and the log belong to the caller.
	Launch(spec RunSpec) *exec.Cmd

	// Resume returns the argv for an interactive session with a person. It is
	// argv and not a command, because a command of the person in section 9.2
	// can put it in a new terminal window.
	Resume(session string) []string

	// SessionID returns the id of the session a finished run used, so a person
	// can resume it later. out is what the run wrote. An agent that accepts a
	// supplied id returns spec.Session and ignores out; an agent that makes
	// its own reads the id back from out.
	SessionID(spec RunSpec, out []byte) (string, error)

	// Name returns the name of the agent, for a message to a person.
	Name() string
}
