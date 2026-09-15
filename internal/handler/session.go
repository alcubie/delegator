package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"os/exec"
	"sync"

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
	turn   sync.Mutex
}

// An outcome is how a turn ended: the reason the agent gave for stopping, or
// the error that stopped it.
type outcome struct {
	stop acp.StopReason
	err  error
}

// eventRoom is how many events the client can keep before the drain has to
// take one. It is a turn's worth of permissions, so an agent asking one after
// another does not wait on the reader between them.
const eventRoom = 64

// open runs the agent's command in cwd, which must be an absolute path, and
// agrees the protocol with it. The agent's stderr goes to stderr, which is
// the only place an ACP agent has to report what it cannot say in the
// protocol. The client offers the agent the files of the machine and no
// terminal.
//
// The process is ended before open gives an error, so a failed start leaves
// nothing running. What it gives back has no session yet: the caller asks the
// agent for the one it wants.
func open(ctx context.Context, kind Kind, policy Policy, cwd string, stderr io.Writer) (*Session, error) {
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
	return s, nil
}

// Start runs the agent's command in cwd and opens a new session there, with
// no MCP servers.
//
// The process is ended before Start gives an error, so a failed start leaves
// nothing running.
func Start(ctx context.Context, kind Kind, policy Policy, cwd string, stderr io.Writer) (*Session, error) {
	s, err := open(ctx, kind, policy, cwd, stderr)
	if err != nil {
		return nil, err
	}
	r, err := s.conn.NewSession(ctx, acp.NewSessionRequest{Cwd: cwd, McpServers: []acp.McpServer{}})
	if err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("open a session of agent %q in %s: %w", kind.Name, cwd, err)
	}
	s.id = r.SessionId
	return s, nil
}

// Prompt sends text to the agent and gives the turn it takes as a sequence of
// events: what it says, what it thinks, the tools it runs, and the
// permissions the policy answered for it, in the order they happened. The
// last event ends the sequence, and is a result with the reason the agent
// stopped, or an error with what went wrong, which is given as the error of
// the sequence as well.
//
// Cancelling the context tells the agent to stop and the sequence ends with
// the cancelled stop reason, which is a turn that ended and not a failure. A
// caller that stops reading stops the turn the same way. Either way the turn
// ends when the agent says it has ended, so an agent that answers a stop with
// nothing ends the sequence only when Close takes the process away.
//
// One session takes one turn at a time. A second prompt waits for the first
// to end, because the agent has one session and the events of two turns down
// one channel could not be told apart.
func (s *Session) Prompt(ctx context.Context, text string) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		s.turn.Lock()
		defer s.turn.Unlock()
		// The request the agent answers does not carry the caller's context.
		// A cancelled request would end the wait while the agent was still
		// reporting the turn it is stopping, and the updates it had yet to
		// send would arrive in the middle of the turn after this one.
		request := context.WithoutCancel(ctx)
		done := make(chan outcome, 1)
		go func() {
			r, err := s.conn.Prompt(request, acp.PromptRequest{
				SessionId: s.id,
				Prompt:    []acp.ContentBlock{acp.TextBlock(text)},
			})
			done <- outcome{stop: r.StopReason, err: err}
		}()
		stopping := ctx.Done()
		for {
			select {
			case e := <-s.events:
				if !yield(e, nil) {
					s.stop(request)
					s.abandon(done)
					return
				}
			case <-stopping:
				stopping = nil // the turn is stopped once, and stays stopped
				s.stop(request)
			case out := <-done:
				s.end(out, yield)
				return
			}
		}
	}
}

// stop asks the agent to end the turn. The agent answers the prompt all the
// same, so the sequence ends where the turn ends and not where the caller
// stopped waiting for it.
func (s *Session) stop(ctx context.Context) {
	_ = s.conn.Cancel(ctx, acp.CancelNotification{SessionId: s.id})
}

// end gives the events the agent sent last and then the one that ends the
// sequence.
func (s *Session) end(out outcome, yield func(Event, error) bool) {
	for _, e := range s.last() {
		if !yield(e, nil) {
			return
		}
	}
	if out.err != nil {
		yield(Event{Type: TypeError, Err: out.err.Error()}, out.err)
		return
	}
	yield(Event{Type: TypeResult, Status: string(out.stop)}, nil)
}

// abandon drops what the agent goes on sending until the turn is over, so
// that the turn after this one starts on an empty channel. It also keeps the
// agent from blocking on a report that nothing is taking.
func (s *Session) abandon(done <-chan outcome) {
	for {
		select {
		case <-s.events:
		case <-done:
			s.last()
			return
		}
	}
}

// last takes the events the agent sent before it answered the prompt and that
// the turn has not taken yet. The SDK answers only once every notification
// the agent sent before that answer has been handled, so what the channel
// holds when the answer comes is the end of the turn and nothing is coming
// after it. A turn has one reader, so what len reports is there to take.
func (s *Session) last() []Event {
	events := make([]Event, 0, len(s.events))
	for len(s.events) > 0 {
		events = append(events, <-s.events)
	}
	return events
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
