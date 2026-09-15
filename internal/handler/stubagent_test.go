package handler

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	acp "github.com/coder/acp-go-sdk"
)

// The stub agent is an ACP agent built on the SDK's agent side and run as a
// second copy of the test binary, so a test exercises Start over a real
// process and a real protocol exchange without a real agent. It writes what
// the client told it into a file the test reads, because a pipe the client
// owns is not a channel the test can wait on.
const (
	stubSessionID = "sess-stub"
	stubHello     = "stub agent started"
)

// The two sets of permission options the stub agent offers, named by an
// argument of the stub so that a test says what the agent offers. In the full
// set the option that allows once comes after the one that allows always, and
// the same for the two that reject, so a client that takes the first option
// it can use is told apart from one that takes the narrower answer.
const (
	stubFullOptions   = "full"
	stubAlwaysOptions = "always"
)

// What the stub agent's prompt does, named by an argument of the stub so
// that a test says which turn it wants.
const (
	stubTurnPermissions = "permissions" // ask for two permissions and end the turn
	stubTurnUpdates     = "updates"     // send one update of every kind and end the turn
	stubTurnHang        = "hang"        // send one chunk and wait to be cancelled
	stubTurnError       = "error"       // fail the turn
)

// What the stub agent does with a load, named by an argument of the stub. It
// implements the loader whatever it says, so a request that reaches an agent
// that says it cannot load is answered rather than refused, and a test sees
// whether the client asked at all.
const (
	stubLoads     = "loads"      // say it loads, and replay two updates
	stubLoadsLong = "loads-long" // say it loads, and replay more than the channel holds
	stubNoLoad    = "no-load"    // say it cannot load
)

// The two updates the stub agent replays when it loads a session, which stand
// for the turn the session already took.
const (
	stubReplayFirst  = "the session said this before"
	stubReplaySecond = "and then it said this"
)

// stubLongHistory is how many updates the long replay sends. It is more than
// the channel between the client and a turn holds, which is the history of
// any session a person has worked in for a while.
const stubLongHistory = 3 * eventRoom

// stubHistoryLine is the text of one update of the long history. The updates
// are numbered so that a test reads back the order as well as the count.
func stubHistoryLine(i int) string { return fmt.Sprintf("history line %d", i) }

// What the updates turn sends. The command has a second line, so a test sees
// that a summary is one line of the command and not all of it.
const (
	stubThought     = "the tests come first"
	stubReadTitle   = "Read internal/cli/run.go"
	stubReadPath    = "/repo/internal/cli/run.go"
	stubCommand     = "go test ./internal/handler\nmake check"
	stubCommandLine = "go test ./internal/handler…"
	stubFailure     = "the stub agent has no model"
)

// What the stub agent says of an id it never issued. It names neither the id
// nor the agent, so a test that wants both in the error is reading what the
// handler put there and not what the agent happened to say.
const stubUnknownSession = "there is no session of that id"

// The ids of the options the stub agent offers. A decision it records is the
// id the client selected, or stubCancelled when the client took no option.
const (
	stubAllowOnce    = "allow-once"
	stubAllowAlways  = "allow-always"
	stubRejectOnce   = "reject-once"
	stubRejectAlways = "reject-always"
	stubCancelled    = "cancelled"
	stubEditTitle    = "Edit internal/cli/run.go"
	stubExecuteTitle = "Run the tests"
	stubPermissions  = 2 // the permissions one prompt of the stub agent asks for
)

// stubRecord is what the stub agent saw: the capabilities of the initialize,
// the directory of the session, and the option the client selected for each
// permission it asked for, in the order it asked.
type stubRecord struct {
	Fs        acp.FileSystemCapabilities `json:"fs"`
	Terminal  bool                       `json:"terminal"`
	Cwd       string                     `json:"cwd"`
	Decisions []string                   `json:"decisions"`
}

// stubKind gives the Kind that starts the stub agent offering one of the sets
// of permission options, taking one of the turns and saying whether it can
// load a session, and the path of the file it records into.
func stubKind(t *testing.T, options, turn, loading string) (Kind, string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	record := filepath.Join(t.TempDir(), "record.json")
	argv := []string{self, "-test.run=TestStubAgent", "stub", record, options, turn, loading}
	return Kind{Name: "stub", Argv: argv}, record
}

// readRecord reads what the stub agent wrote.
func readRecord(t *testing.T, path string) stubRecord {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the stub agent recorded nothing: %v", err)
	}
	var r stubRecord
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("the record is not JSON: %v", err)
	}
	return r
}

// TestStubAgent is the stub agent and not a test. It runs only in the copy of
// the test binary that stubKind starts, which names itself in the arguments
// after the flags; every other run skips it.
func TestStubAgent(t *testing.T) {
	args := flag.Args()
	if len(args) != 5 || args[0] != "stub" {
		t.Skip("this run is not the stub agent")
	}
	fmt.Fprintln(os.Stderr, stubHello)
	agent := &stubAgent{record: args[1], options: args[2], turn: args[3], loading: args[4]}
	conn := acp.NewAgentSideConnection(agent, os.Stdout, os.Stdin)
	agent.conn = conn
	<-conn.Done()
	os.Exit(0)
}

type stubAgent struct {
	record    string
	options   string
	turn      string
	loading   string
	conn      *acp.AgentSideConnection
	fs        acp.FileSystemCapabilities
	term      bool
	cwd       string
	decisions []string
}

var (
	_ acp.Agent       = (*stubAgent)(nil)
	_ acp.AgentLoader = (*stubAgent)(nil)
)

func (a *stubAgent) Initialize(_ context.Context, p acp.InitializeRequest) (acp.InitializeResponse, error) {
	a.fs, a.term = p.ClientCapabilities.Fs, p.ClientCapabilities.Terminal
	return acp.InitializeResponse{
		ProtocolVersion:   acp.ProtocolVersionNumber,
		AgentCapabilities: acp.AgentCapabilities{LoadSession: a.loading != stubNoLoad},
	}, nil
}

func (a *stubAgent) NewSession(_ context.Context, p acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	a.cwd = p.Cwd
	if err := a.write(); err != nil {
		return acp.NewSessionResponse{}, err
	}
	return acp.NewSessionResponse{SessionId: acp.SessionId(stubSessionID)}, nil
}

// LoadSession replays the two updates of the session's history and then
// answers, which is the order the protocol asks for, and refuses an id it
// never issued. It records the directory first, so a test that expects the
// load to be refused before it is sent sees that nothing was recorded.
func (a *stubAgent) LoadSession(ctx context.Context, p acp.LoadSessionRequest) (acp.LoadSessionResponse, error) {
	a.cwd = p.Cwd
	if err := a.write(); err != nil {
		return acp.LoadSessionResponse{}, err
	}
	if string(p.SessionId) != stubSessionID {
		return acp.LoadSessionResponse{}, errors.New(stubUnknownSession)
	}
	for _, text := range a.history() {
		u := acp.UpdateAgentMessageText(text)
		if err := a.conn.SessionUpdate(ctx, acp.SessionNotification{SessionId: p.SessionId, Update: u}); err != nil {
			return acp.LoadSessionResponse{}, err
		}
	}
	return acp.LoadSessionResponse{}, nil
}

// history is the text the stub agent replays, which is two updates or a whole
// session's worth of them.
func (a *stubAgent) history() []string {
	if a.loading != stubLoadsLong {
		return []string{stubReplayFirst, stubReplaySecond}
	}
	lines := make([]string, 0, stubLongHistory)
	for i := range stubLongHistory {
		lines = append(lines, stubHistoryLine(i))
	}
	return lines
}

// Prompt takes the turn the stub agent was started with.
func (a *stubAgent) Prompt(ctx context.Context, p acp.PromptRequest) (acp.PromptResponse, error) {
	switch a.turn {
	case stubTurnPermissions:
		return a.permissions(ctx, p)
	case stubTurnUpdates:
		return a.updates(ctx, p)
	case stubTurnHang:
		return a.hang(ctx, p)
	case stubTurnError:
		return acp.PromptResponse{}, errors.New(stubFailure)
	}
	return acp.PromptResponse{}, fmt.Errorf("the stub agent has no turn %q", a.turn)
}

// permissions asks permission for a tool of kind edit and then for one of kind
// execute, and records what it was answered. It writes the record before it
// replies, so the test reads the decisions as soon as the prompt is over.
func (a *stubAgent) permissions(ctx context.Context, p acp.PromptRequest) (acp.PromptResponse, error) {
	for _, ask := range []struct {
		id    string
		kind  acp.ToolKind
		title string
	}{
		{"call-1", acp.ToolKindEdit, stubEditTitle},
		{"call-2", acp.ToolKindExecute, stubExecuteTitle},
	} {
		r, err := a.conn.RequestPermission(ctx, acp.RequestPermissionRequest{
			SessionId: p.SessionId,
			ToolCall: acp.ToolCallUpdate{
				ToolCallId: acp.ToolCallId(ask.id),
				Kind:       acp.Ptr(ask.kind),
				Title:      acp.Ptr(ask.title),
			},
			Options: a.permissionOptions(),
		})
		if err != nil {
			return acp.PromptResponse{}, err
		}
		decision := stubCancelled
		if r.Outcome.Selected != nil {
			decision = string(r.Outcome.Selected.OptionId)
		}
		a.decisions = append(a.decisions, decision)
	}
	if err := a.write(); err != nil {
		return acp.PromptResponse{}, err
	}
	return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}

// updates sends one update of each kind the handler maps and one plan, which
// it does not. The first chunk is the text of the prompt, so a test that runs
// two prompts at once tells the events of one turn from the other's.
func (a *stubAgent) updates(ctx context.Context, p acp.PromptRequest) (acp.PromptResponse, error) {
	for _, u := range []acp.SessionUpdate{
		acp.UpdateAgentMessageText(promptText(p)),
		acp.UpdateAgentThoughtText(stubThought),
		acp.StartToolCall("call-1", stubReadTitle,
			acp.WithStartKind(acp.ToolKindRead),
			acp.WithStartStatus(acp.ToolCallStatusPending),
			acp.WithStartLocations([]acp.ToolCallLocation{{Path: stubReadPath}})),
		acp.UpdateToolCall("call-1", acp.WithUpdateStatus(acp.ToolCallStatusInProgress)),
		acp.UpdatePlan(acp.PlanEntry{
			Content:  "run the tests",
			Priority: acp.PlanEntryPriorityMedium,
			Status:   acp.PlanEntryStatusInProgress,
		}),
		acp.StartToolCall("call-2", stubExecuteTitle,
			acp.WithStartKind(acp.ToolKindExecute),
			acp.WithStartStatus(acp.ToolCallStatusPending),
			acp.WithStartRawInput(map[string]any{"command": stubCommand})),
	} {
		if err := a.conn.SessionUpdate(ctx, acp.SessionNotification{SessionId: p.SessionId, Update: u}); err != nil {
			return acp.PromptResponse{}, err
		}
	}
	return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}

// hang sends one chunk and then waits, so that a test cancels a turn that has
// started. The turn ends when the client cancels the request.
func (a *stubAgent) hang(ctx context.Context, p acp.PromptRequest) (acp.PromptResponse, error) {
	u := acp.UpdateAgentMessageText(promptText(p))
	if err := a.conn.SessionUpdate(ctx, acp.SessionNotification{SessionId: p.SessionId, Update: u}); err != nil {
		return acp.PromptResponse{}, err
	}
	<-ctx.Done()
	return acp.PromptResponse{StopReason: acp.StopReasonCancelled}, nil
}

// promptText is the text of the first block of a prompt, which is all that
// the handler sends.
func promptText(p acp.PromptRequest) string {
	if len(p.Prompt) == 0 || p.Prompt[0].Text == nil {
		return ""
	}
	return p.Prompt[0].Text.Text
}

// permissionOptions gives the set of options the stub was started with.
func (a *stubAgent) permissionOptions() []acp.PermissionOption {
	if a.options == stubAlwaysOptions {
		return []acp.PermissionOption{
			{Kind: acp.PermissionOptionKindAllowAlways, Name: "Always allow", OptionId: stubAllowAlways},
		}
	}
	return []acp.PermissionOption{
		{Kind: acp.PermissionOptionKindAllowAlways, Name: "Always allow", OptionId: stubAllowAlways},
		{Kind: acp.PermissionOptionKindAllowOnce, Name: "Allow", OptionId: stubAllowOnce},
		{Kind: acp.PermissionOptionKindRejectAlways, Name: "Always reject", OptionId: stubRejectAlways},
		{Kind: acp.PermissionOptionKindRejectOnce, Name: "Reject", OptionId: stubRejectOnce},
	}
}

// write puts everything the stub agent has seen into its record file.
func (a *stubAgent) write() error {
	b, err := json.Marshal(stubRecord{Fs: a.fs, Terminal: a.term, Cwd: a.cwd, Decisions: a.decisions})
	if err != nil {
		return err
	}
	return os.WriteFile(a.record, b, 0o644)
}

func (a *stubAgent) Authenticate(context.Context, acp.AuthenticateRequest) (acp.AuthenticateResponse, error) {
	return acp.AuthenticateResponse{}, nil
}
func (a *stubAgent) Logout(context.Context, acp.LogoutRequest) (acp.LogoutResponse, error) {
	return acp.LogoutResponse{}, nil
}
func (a *stubAgent) Cancel(context.Context, acp.CancelNotification) error { return nil }
func (a *stubAgent) CloseSession(context.Context, acp.CloseSessionRequest) (acp.CloseSessionResponse, error) {
	return acp.CloseSessionResponse{}, nil
}
func (a *stubAgent) ListSessions(context.Context, acp.ListSessionsRequest) (acp.ListSessionsResponse, error) {
	return acp.ListSessionsResponse{}, nil
}
func (a *stubAgent) ResumeSession(context.Context, acp.ResumeSessionRequest) (acp.ResumeSessionResponse, error) {
	return acp.ResumeSessionResponse{}, nil
}
func (a *stubAgent) SetSessionConfigOption(context.Context, acp.SetSessionConfigOptionRequest) (acp.SetSessionConfigOptionResponse, error) {
	return acp.SetSessionConfigOptionResponse{}, nil
}
func (a *stubAgent) SetSessionMode(context.Context, acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	return acp.SetSessionModeResponse{}, nil
}
