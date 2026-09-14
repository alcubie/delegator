package handler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	acp "github.com/coder/acp-go-sdk"
)

// client answers what an agent asks of delegator: the files it reads and
// writes, the permission it asks for a tool, and the updates it sends as a
// turn goes on.
type client struct {
	policy Policy
}

var _ acp.Client = (*client)(nil)

// ReadTextFile gives the agent a file of the machine. The protocol says the
// path is absolute, and a relative one is refused rather than resolved:
// delegator's directory is not the agent's, so the file it would find is not
// the file the agent asked for.
func (c *client) ReadTextFile(_ context.Context, p acp.ReadTextFileRequest) (acp.ReadTextFileResponse, error) {
	if !filepath.IsAbs(p.Path) {
		return acp.ReadTextFileResponse{}, fmt.Errorf("read %s: the path is not absolute", p.Path)
	}
	b, err := os.ReadFile(p.Path)
	if err != nil {
		return acp.ReadTextFileResponse{}, err
	}
	return acp.ReadTextFileResponse{Content: string(b)}, nil
}

// WriteTextFile writes a file for the agent, and makes the directories above
// it, because an agent that adds a file to a tree it is building does not
// make the directory first.
func (c *client) WriteTextFile(_ context.Context, p acp.WriteTextFileRequest) (acp.WriteTextFileResponse, error) {
	if !filepath.IsAbs(p.Path) {
		return acp.WriteTextFileResponse{}, fmt.Errorf("write %s: the path is not absolute", p.Path)
	}
	if err := os.MkdirAll(filepath.Dir(p.Path), 0o755); err != nil {
		return acp.WriteTextFileResponse{}, err
	}
	return acp.WriteTextFileResponse{}, os.WriteFile(p.Path, []byte(p.Content), 0o644)
}

// RequestPermission cancels every request. The options an agent offers are
// not read yet, so the only answer the client can give is none.
func (c *client) RequestPermission(_ context.Context, _ acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	return acp.RequestPermissionResponse{
		Outcome: acp.RequestPermissionOutcome{Cancelled: &acp.RequestPermissionOutcomeCancelled{}},
	}, nil
}

// SessionUpdate drops what the agent reports.
func (c *client) SessionUpdate(context.Context, acp.SessionNotification) error { return nil }

// The terminal methods are refused. The client offers no terminal in its
// capabilities, so an agent that asks for one is asking for what it was told
// is not there, and it runs its commands with its own shell instead.
func (c *client) CreateTerminal(context.Context, acp.CreateTerminalRequest) (acp.CreateTerminalResponse, error) {
	return acp.CreateTerminalResponse{}, errNoTerminal
}

func (c *client) TerminalOutput(context.Context, acp.TerminalOutputRequest) (acp.TerminalOutputResponse, error) {
	return acp.TerminalOutputResponse{}, errNoTerminal
}

func (c *client) WaitForTerminalExit(context.Context, acp.WaitForTerminalExitRequest) (acp.WaitForTerminalExitResponse, error) {
	return acp.WaitForTerminalExitResponse{}, errNoTerminal
}

func (c *client) KillTerminal(context.Context, acp.KillTerminalRequest) (acp.KillTerminalResponse, error) {
	return acp.KillTerminalResponse{}, errNoTerminal
}

func (c *client) ReleaseTerminal(context.Context, acp.ReleaseTerminalRequest) (acp.ReleaseTerminalResponse, error) {
	return acp.ReleaseTerminalResponse{}, errNoTerminal
}

var errNoTerminal = fmt.Errorf("terminal not supported")
