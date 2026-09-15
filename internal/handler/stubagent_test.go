package handler

import (
	"context"
	"encoding/json"
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
// of permission options, and the path of the file it records into.
func stubKind(t *testing.T, options string) (Kind, string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	record := filepath.Join(t.TempDir(), "record.json")
	return Kind{Name: "stub", Argv: []string{self, "-test.run=TestStubAgent", "stub", record, options}}, record
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
	if len(args) != 3 || args[0] != "stub" {
		t.Skip("this run is not the stub agent")
	}
	fmt.Fprintln(os.Stderr, stubHello)
	agent := &stubAgent{record: args[1], options: args[2]}
	conn := acp.NewAgentSideConnection(agent, os.Stdout, os.Stdin)
	agent.conn = conn
	<-conn.Done()
	os.Exit(0)
}

type stubAgent struct {
	record    string
	options   string
	conn      *acp.AgentSideConnection
	fs        acp.FileSystemCapabilities
	term      bool
	cwd       string
	decisions []string
}

var _ acp.Agent = (*stubAgent)(nil)

func (a *stubAgent) Initialize(_ context.Context, p acp.InitializeRequest) (acp.InitializeResponse, error) {
	a.fs, a.term = p.ClientCapabilities.Fs, p.ClientCapabilities.Terminal
	return acp.InitializeResponse{ProtocolVersion: acp.ProtocolVersionNumber}, nil
}

func (a *stubAgent) NewSession(_ context.Context, p acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	a.cwd = p.Cwd
	if err := a.write(); err != nil {
		return acp.NewSessionResponse{}, err
	}
	return acp.NewSessionResponse{SessionId: acp.SessionId(stubSessionID)}, nil
}

// Prompt asks permission for a tool of kind edit and then for one of kind
// execute, and records what it was answered. It writes the record before it
// replies, so the test reads the decisions as soon as the prompt is over.
func (a *stubAgent) Prompt(ctx context.Context, p acp.PromptRequest) (acp.PromptResponse, error) {
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
