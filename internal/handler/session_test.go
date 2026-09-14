package handler

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

// start opens a session on the stub agent and closes it when the test ends,
// and gives back the record of what the agent saw and the agent's stderr.
func start(t *testing.T, cwd string) (*Session, string, *bytes.Buffer) {
	t.Helper()
	return startPolicy(t, cwd, Policy{}, stubFullOptions)
}

// startPolicy opens a session on the stub agent that answers by the policy
// and offers one of the sets of permission options.
func startPolicy(t *testing.T, cwd string, policy Policy, options string) (*Session, string, *bytes.Buffer) {
	t.Helper()
	kind, record := stubKind(t, options)
	var stderr bytes.Buffer
	s, err := Start(t.Context(), kind, policy, cwd, &stderr)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, record, &stderr
}

// prompt sends one prompt to the stub agent, which asks for its two
// permissions, and gives back what the agent recorded of the decisions and
// the events the session kept.
func prompt(t *testing.T, policy Policy, options string) ([]string, []Event) {
	t.Helper()
	s, record, _ := startPolicy(t, t.TempDir(), policy, options)
	if _, err := s.conn.Prompt(t.Context(), acp.PromptRequest{
		SessionId: s.id,
		Prompt:    []acp.ContentBlock{acp.TextBlock("do the work")},
	}); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	return readRecord(t, record).Decisions, drain(t, s, stubPermissions)
}

// drain takes the events the session kept, and fails rather than waiting for
// one that is not coming.
func drain(t *testing.T, s *Session, want int) []Event {
	t.Helper()
	var got []Event
	for len(got) < want {
		select {
		case e := <-s.events:
			got = append(got, e)
		case <-time.After(5 * time.Second):
			t.Fatalf("the session kept %d events, and the test wants %d: %+v", len(got), want, got)
		}
	}
	return got
}

func TestStartOpensTheSessionTheAgentIssued(t *testing.T) {
	s, _, _ := start(t, t.TempDir())
	if s.ID() != stubSessionID {
		t.Errorf("ID() = %q, and the agent issued %q", s.ID(), stubSessionID)
	}
}

func TestStartAsksForFilesAndNotForATerminal(t *testing.T) {
	_, record, _ := start(t, t.TempDir())
	r := readRecord(t, record)
	if !r.Fs.ReadTextFile || !r.Fs.WriteTextFile {
		t.Errorf("the agent was offered fs %+v, and it wants both", r.Fs)
	}
	if r.Terminal {
		t.Error("the agent was offered a terminal, and the client serves none")
	}
}

func TestStartOpensTheSessionInTheDirectory(t *testing.T) {
	dir := t.TempDir()
	_, record, _ := start(t, dir)
	if got := readRecord(t, record).Cwd; got != dir {
		t.Errorf("the session cwd is %q, and the directory is %q", got, dir)
	}
}

func TestCloseEndsTheAgent(t *testing.T) {
	s, _, stderr := start(t, t.TempDir())
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := s.cmd.Process.Signal(os.Interrupt); !errors.Is(err, os.ErrProcessDone) {
		t.Errorf("the agent answered a signal after Close with %v, and it should be done", err)
	}
	if !strings.Contains(stderr.String(), stubHello) {
		t.Errorf("the agent's stderr is %q, and it wrote %q", stderr.String(), stubHello)
	}
}

func TestStartNamesACommandThatIsNotThere(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-agent")
	_, err := Start(t.Context(), Kind{Name: "stub", Argv: []string{missing}}, Policy{}, t.TempDir(), io.Discard)
	if err == nil {
		t.Fatal("Start found an agent that is not there")
	}
	if !strings.Contains(err.Error(), "no-such-agent") {
		t.Errorf("the error is %v, and it does not name the command", err)
	}
}

func TestStartRefusesAKindWithNoCommand(t *testing.T) {
	_, err := Start(t.Context(), Kind{Name: "empty"}, Policy{}, t.TempDir(), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("Start of a kind with no command gave %v, and the error should name the kind", err)
	}
}

func TestAllowKindsAllowsTheKindAndRejectsTheRest(t *testing.T) {
	decisions, events := prompt(t, AllowKinds(acp.ToolKindEdit), stubFullOptions)
	want := []string{stubAllowOnce, stubRejectOnce}
	if len(decisions) != 2 || decisions[0] != want[0] || decisions[1] != want[1] {
		t.Errorf("the agent was answered %v, want %v", decisions, want)
	}
	wantEvents := []Event{
		{Type: TypePermission, Tool: stubEditTitle, Kind: string(acp.ToolKindEdit), Status: StatusAllowed},
		{Type: TypePermission, Tool: stubExecuteTitle, Kind: string(acp.ToolKindExecute), Status: StatusRejected},
	}
	for i, want := range wantEvents {
		if events[i] != want {
			t.Errorf("event %d is %+v, want %+v", i, events[i], want)
		}
	}
}

func TestDenyRejectsEveryRequest(t *testing.T) {
	decisions, events := prompt(t, Deny(), stubFullOptions)
	for i, got := range decisions {
		if got != stubRejectOnce {
			t.Errorf("request %d was answered %q, want %q", i, got, stubRejectOnce)
		}
		if events[i].Status != StatusRejected {
			t.Errorf("event %d is %+v, want it rejected", i, events[i])
		}
	}
}

func TestAllowAllTakesTheOptionThatAllowsAlwaysWhenItIsTheOnlyOne(t *testing.T) {
	decisions, events := prompt(t, AllowAll(), stubAlwaysOptions)
	for i, got := range decisions {
		if got != stubAllowAlways {
			t.Errorf("request %d was answered %q, want %q", i, got, stubAllowAlways)
		}
		if events[i].Status != StatusAllowed {
			t.Errorf("event %d is %+v, want it allowed", i, events[i])
		}
	}
}
