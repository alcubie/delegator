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

// stubRecord is what the stub agent saw: the capabilities of the initialize
// and the directory of the session.
type stubRecord struct {
	Fs       acp.FileSystemCapabilities `json:"fs"`
	Terminal bool                       `json:"terminal"`
	Cwd      string                     `json:"cwd"`
}

// stubKind gives the Kind that starts the stub agent, and the path of the
// file it records into.
func stubKind(t *testing.T) (Kind, string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	record := filepath.Join(t.TempDir(), "record.json")
	return Kind{Name: "stub", Argv: []string{self, "-test.run=TestStubAgent", "stub", record}}, record
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
	if len(args) != 2 || args[0] != "stub" {
		t.Skip("this run is not the stub agent")
	}
	fmt.Fprintln(os.Stderr, stubHello)
	agent := &stubAgent{record: args[1]}
	<-acp.NewAgentSideConnection(agent, os.Stdout, os.Stdin).Done()
	os.Exit(0)
}

type stubAgent struct {
	record string
	fs     acp.FileSystemCapabilities
	term   bool
}

var _ acp.Agent = (*stubAgent)(nil)

func (a *stubAgent) Initialize(_ context.Context, p acp.InitializeRequest) (acp.InitializeResponse, error) {
	a.fs, a.term = p.ClientCapabilities.Fs, p.ClientCapabilities.Terminal
	return acp.InitializeResponse{ProtocolVersion: acp.ProtocolVersionNumber}, nil
}

func (a *stubAgent) NewSession(_ context.Context, p acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	b, err := json.Marshal(stubRecord{Fs: a.fs, Terminal: a.term, Cwd: p.Cwd})
	if err != nil {
		return acp.NewSessionResponse{}, err
	}
	if err := os.WriteFile(a.record, b, 0o644); err != nil {
		return acp.NewSessionResponse{}, err
	}
	return acp.NewSessionResponse{SessionId: acp.SessionId(stubSessionID)}, nil
}

func (a *stubAgent) Prompt(context.Context, acp.PromptRequest) (acp.PromptResponse, error) {
	return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
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
