package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"

	acp "github.com/coder/acp-go-sdk"
)

// A Kind is one agent: the name a person selects it by, the command that
// starts its ACP server on stdio, and the command that opens one of its
// sessions in a terminal.
type Kind struct {
	Name   string
	Argv   []string
	Resume []string
}

// A Policy decides how a permission request is answered. Allow is given the
// kind of tool the agent wants to run and the line it shows for it. A Policy
// with no Allow allows nothing, so a caller that forgets one gets the answer
// that costs least.
type Policy struct {
	Allow func(kind acp.ToolKind, title string) bool
}

// AllowAll allows every tool. It is delegator's policy: a run of a ticket has
// a worktree of its own, and a person who reads the run after it is over is
// not there to answer a question while it goes.
func AllowAll() Policy {
	return Policy{Allow: func(acp.ToolKind, string) bool { return true }}
}

// AllowKinds allows a tool of one of the kinds and rejects the rest.
func AllowKinds(kinds ...acp.ToolKind) Policy {
	allowed := make(map[acp.ToolKind]bool, len(kinds))
	for _, k := range kinds {
		allowed[k] = true
	}
	return Policy{Allow: func(kind acp.ToolKind, _ string) bool { return allowed[kind] }}
}

// Deny rejects every tool.
func Deny() Policy {
	return Policy{Allow: func(acp.ToolKind, string) bool { return false }}
}

// A Session is one agent process and one ACP session in a directory. It holds
// the process so that Close can end it: an agent that outlives the run that
// started it goes on editing a worktree that nothing is watching.
//
// The client puts what it decides on its own onto events, which a prompt
// drains into the stream it gives the caller.
type Session struct {
	cmd    *exec.Cmd
	conn   *acp.ClientSideConnection
	id     acp.SessionId
	events chan Event
}

// eventRoom is how many events the client can keep before the drain has to
// take one. It is a turn's worth of permissions, so an agent asking one after
// another does not wait on the reader between them.
const eventRoom = 64

// Start runs the agent's command in cwd, which must be an absolute path, and
// opens a session there. The agent's stderr goes to stderr, which is the only
// place an ACP agent has to report what it cannot say in the protocol. The
// client offers the agent the files of the machine and no terminal, and gives
// it no MCP servers.
//
// The process is ended before Start gives an error, so a failed start leaves
// nothing running.
func Start(ctx context.Context, kind Kind, policy Policy, cwd string, stderr io.Writer) (*Session, error) {
	if len(kind.Argv) == 0 {
		return nil, fmt.Errorf("agent %q has no command", kind.Name)
	}
	cmd := exec.Command(kind.Argv[0], kind.Argv[1:]...)
	cmd.Dir = cwd
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start agent %q: %w", kind.Name, err)
	}
	s := &Session{cmd: cmd, events: make(chan Event, eventRoom)}
	s.conn = acp.NewClientSideConnection(&client{policy: policy, events: s.events}, stdin, stdout)
	if _, err := s.conn.Initialize(ctx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientCapabilities: acp.ClientCapabilities{
			Fs:       acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true},
			Terminal: false,
		},
	}); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("initialize agent %q: %w", kind.Name, err)
	}
	r, err := s.conn.NewSession(ctx, acp.NewSessionRequest{Cwd: cwd, McpServers: []acp.McpServer{}})
	if err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("open a session of agent %q in %s: %w", kind.Name, cwd, err)
	}
	s.id = r.SessionId
	return s, nil
}

// ID is the id the agent gave the session. Claude's is the id of the Claude
// session, so a ticket keeps it and a person resumes the session by it.
func (s *Session) ID() string { return string(s.id) }

// Close kills the agent and waits for it. The kill is what ends it, so the
// status it dies of is not a failure of the session and is not reported.
func (s *Session) Close() error {
	_ = s.cmd.Process.Kill()
	var exit *exec.ExitError
	if err := s.cmd.Wait(); err != nil && !errors.As(err, &exit) {
		return err
	}
	return nil
}
