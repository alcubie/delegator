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

// Policy decides permission requests from the tool kind and displayed title.
// A nil Allow denies all requests.
type Policy struct {
	Allow func(kind acp.ToolKind, title string) bool
}

// SessionOptions are the parts of an agent session that are outside its
// working directory. AdditionalDirectories become ACP workspace roots, and
// Environment is added to the environment inherited by the agent process.
type SessionOptions struct {
	AdditionalDirectories []string
	Environment           []string
}

// AllowAll permits every tool for unattended ticket runs, where no user is
// available to answer permission requests.
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

// Session owns an agent process and an ACP session in a working directory.
// Close terminates the process so it cannot keep editing after the run ends.
//
// Prompt drains agent updates and client decisions into one event stream. A
// loaded session also replays its history at the start of the first prompt.
type Session struct {
	cmd    *exec.Cmd
	conn   *acp.ClientSideConnection
	caps   acp.AgentCapabilities
	id     acp.SessionId
	loaded bool
	replay []Event
	events chan Event
	usage  *Usage
	turn   sync.Mutex
}

// outcome holds a turn's stop reason or error.
type outcome struct {
	stop  acp.StopReason
	usage *Usage
	err   error
}

// eventRoom bounds buffered client events, allowing short bursts without
// waiting for the consumer.
const eventRoom = 64

// open starts the agent in absolute cwd and negotiates ACP capabilities for
// file access, without terminal support. Agent diagnostics go to stderr. It
// returns a process without a session; on failure it terminates the process.
func open(ctx context.Context, name string, argv []string, policy Policy, cwd string, options SessionOptions, stderr io.Writer) (*Session, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("agent %q has no command", name)
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = cwd
	cmd.Env = append(cmd.Environ(), options.Environment...)
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
		return nil, fmt.Errorf("start agent %q: %w", name, err)
	}
	s := &Session{cmd: cmd, events: make(chan Event, eventRoom)}
	s.conn = acp.NewClientSideConnection(&client{policy: policy, events: s.events}, stdin, stdout)
	r, err := s.conn.Initialize(ctx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientCapabilities: acp.ClientCapabilities{
			Fs:       acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true},
			Terminal: false,
		},
	})
	if err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("initialize agent %q: %w", name, err)
	}
	s.caps = r.AgentCapabilities
	return s, nil
}

// Start launches the agent in cwd and opens a new session with no MCP
// servers. Any failure terminates the process.
func Start(ctx context.Context, name string, argv []string, policy Policy, cwd string, options SessionOptions, stderr io.Writer) (*Session, error) {
	s, err := open(ctx, name, argv, policy, cwd, options, stderr)
	if err != nil {
		return nil, err
	}
	r, err := s.conn.NewSession(ctx, acp.NewSessionRequest{
		Cwd:                   cwd,
		McpServers:            []acp.McpServer{},
		AdditionalDirectories: options.AdditionalDirectories,
	})
	if err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("open a session of agent %q in %s: %w", name, cwd, err)
	}
	s.id = r.SessionId
	return s, nil
}

// Load launches the agent in cwd and loads a saved session. History updates
// are marked Replay and emitted at the start of the first Prompt stream.
//
// Agents without load support and unknown session IDs return errors. Any
// failure terminates the process.
func Load(ctx context.Context, name string, argv []string, policy Policy, cwd, id string, options SessionOptions, stderr io.Writer) (*Session, error) {
	s, err := open(ctx, name, argv, policy, cwd, options, stderr)
	if err != nil {
		return nil, err
	}
	if !s.caps.LoadSession {
		_ = s.Close()
		return nil, fmt.Errorf("agent %q cannot load a session", name)
	}
	replay, err := s.collect(func() error {
		_, err := s.conn.LoadSession(ctx, acp.LoadSessionRequest{
			Cwd:                   cwd,
			McpServers:            []acp.McpServer{},
			SessionId:             acp.SessionId(id),
			AdditionalDirectories: options.AdditionalDirectories,
		})
		return err
	})
	if err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("load session %s of agent %q in %s: %w", id, name, cwd, err)
	}
	s.replay, s.id, s.loaded = replay, acp.SessionId(id), true
	return s, nil
}

// collect runs f while collecting updates in order, marked as replay. Drain
// concurrently: history may exceed the channel capacity, and the SDK waits
// for notification handlers before returning a response. Once f returns, the
// remaining buffered events complete the replay.
func (s *Session) collect(f func() error) ([]Event, error) {
	collected := make(chan []Event, 1)
	stop := make(chan struct{})
	go func() {
		var got []Event
		for {
			select {
			case e := <-s.events:
				got = append(got, e)
			case <-stop:
				collected <- append(got, s.last()...)
				return
			}
		}
	}()
	err := f()
	close(stop)
	got := <-collected
	for i := range got {
		got[i].Replay = true
	}
	return got, err
}

// Loaded reports whether the session was resumed. Its history appears as
// Replay events at the start of the first prompt.
func (s *Session) Loaded() bool { return s.loaded }

// Prompt sends text and streams agent activity and permission decisions in
// order. The final event is a result with a stop reason or an error, also
// returned as the sequence error.
//
// Context cancellation or stopping iteration requests cancellation. The turn
// still waits for the agent's response; Close is needed if the agent never
// responds. A cancelled stop reason is a result, not a protocol error.
//
// Prompts are serialized so events from separate turns cannot mix.
func (s *Session) Prompt(ctx context.Context, text string) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		s.turn.Lock()
		defer s.turn.Unlock()
		// Consume replay only once, even if the caller stops reading
		// partway through it.
		replay := s.replay
		s.replay = nil
		for _, e := range replay {
			if !yield(e, nil) {
				return
			}
		}
		// Keep the request alive after caller cancellation until the
		// agent finishes responding, so late updates cannot leak into
		// the next turn.
		request := context.WithoutCancel(ctx)
		done := make(chan outcome, 1)
		go func() {
			r, err := s.conn.Prompt(request, acp.PromptRequest{
				SessionId: s.id,
				Prompt:    []acp.ContentBlock{acp.TextBlock(text)},
			})
			done <- outcome{stop: r.StopReason, usage: promptUsage(r.Usage), err: err}
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
				stopping = nil // request cancellation only once
				s.stop(request)
			case out := <-done:
				s.usage = out.usage
				s.end(out, yield)
				return
			}
		}
	}
}

// stop requests cancellation; the prompt response still determines when the
// turn ends.
func (s *Session) stop(ctx context.Context) {
	_ = s.conn.Cancel(ctx, acp.CancelNotification{SessionId: s.id})
}

// end drains remaining events, then emits the turn result.
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

// abandon drains and discards updates until the turn ends, preventing blocked
// senders and leaving the channel empty for the next turn.
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

// last drains events buffered before the prompt response. The SDK finishes
// notification handlers before returning the response, and this turn has the
// only reader, so len gives the remaining event count.
func (s *Session) last() []Event {
	events := make([]Event, 0, len(s.events))
	for len(s.events) > 0 {
		events = append(events, <-s.events)
	}
	return events
}

// ID returns the agent-provided session ID used to resume the conversation.
func (s *Session) ID() string { return string(s.id) }

// Usage returns a copy of the aggregate token usage from the prompt that just
// ended. Nil means the agent returned no Usage object.
func (s *Session) Usage() *Usage {
	if s.usage == nil {
		return nil
	}
	result := *s.usage
	result.CachedWriteTokens = copyInt(result.CachedWriteTokens)
	result.CachedReadTokens = copyInt(result.CachedReadTokens)
	result.ThoughtTokens = copyInt(result.ThoughtTokens)
	return &result
}

// Close kills the agent and waits for it, ignoring the exit status caused by
// that kill.
func (s *Session) Close() error {
	_ = s.cmd.Process.Kill()
	var exit *exec.ExitError
	if err := s.cmd.Wait(); err != nil && !errors.As(err, &exit) {
		return err
	}
	return nil
}
