// Package adapters is the seam for an agent. Delegator drives claude in
// version 1, and a later version can drive a different agent without changing
// anything above this interface.
//
// The interface stays small deliberately. What delegator knows about a run goes
// down in RunSpec, and the one thing it learns from a run comes back through
// SessionID, so a new agent needs no new field above the seam.
//
// Delegator mints no session id. Claude will take a new one at its start, with
// --session-id, and the others will not: one path for every agent is worth more
// than a property one of them has. The session in RunSpec is never a new id.
// It is the id the agent reported through SessionID on an earlier run, and a
// restart gives it back so that the agent continues that conversation.
package adapters

import "os/exec"

// RunSpec is everything a headless run of an agent needs.
type RunSpec struct {
	// Worktree is the directory the agent works in.
	Worktree string

	// Prompt is the first message to the agent.
	Prompt string

	// Session is the conversation the run continues. It is empty for a run
	// that starts one, and dg restart gives the session of the run that
	// failed, so the agent keeps the work that run did.
	Session string
}

// Adapter starts one kind of agent.
type Adapter interface {
	// Launch returns the command for a headless run. The caller starts it and
	// waits, so the timeout and the log belong to the caller.
	Launch(spec RunSpec) *exec.Cmd

	// Resume returns the argv for an interactive session. It is argv rather
	// than a built command because the caller may need to wrap it in a new
	// terminal window.
	Resume(session string) []string

	// SessionID returns the id of the session a run made, parsed from out,
	// which is what the run has written so far. The supervisor calls it after
	// each line the run writes, and stops once it has an id, so a run that
	// stops part way has already left its id on the ticket. Every agent mints
	// its own id and reports it, so there is one path here and not one per
	// agent.
	//
	// An empty id and no error means the run has reported none yet, which
	// after the last line is what a run that died before it said anything
	// looks like.
	SessionID(out []byte) (string, error)

	// Name returns the agent's name, for use in messages.
	Name() string
}
