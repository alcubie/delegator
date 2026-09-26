package handler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	acp "github.com/coder/acp-go-sdk"
)

// client handles agent file access, permission requests, and session updates.
// It emits permission decisions alongside agent events for the caller to
// observe.
type client struct {
	policy Policy
	events chan Event
}

var _ acp.Client = (*client)(nil)

// ReadTextFile reads an absolute path as required by ACP. Relative paths are
// rejected because the client and agent may have different working
// directories.
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

// WriteTextFile writes an agent-requested file, creating parent directories
// as needed.
func (c *client) WriteTextFile(_ context.Context, p acp.WriteTextFileRequest) (acp.WriteTextFileResponse, error) {
	if !filepath.IsAbs(p.Path) {
		return acp.WriteTextFileResponse{}, fmt.Errorf("write %s: the path is not absolute", p.Path)
	}
	if err := os.MkdirAll(filepath.Dir(p.Path), 0o755); err != nil {
		return acp.WriteTextFileResponse{}, err
	}
	return acp.WriteTextFileResponse{}, os.WriteFile(p.Path, []byte(p.Content), 0o644)
}

// RequestPermission applies the policy, preferring a one-time option over a
// persistent option, and emits the decision. If no offered option matches, it
// returns cancelled without emitting a decision.
func (c *client) RequestPermission(ctx context.Context, p acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	kind, title := acp.ToolKindOther, ""
	if p.ToolCall.Kind != nil {
		kind = *p.ToolCall.Kind
	}
	if p.ToolCall.Title != nil {
		title = *p.ToolCall.Title
	}
	answer, status := []acp.PermissionOptionKind{
		acp.PermissionOptionKindRejectOnce,
		acp.PermissionOptionKindRejectAlways,
	}, StatusRejected
	if c.policy.Allow != nil && c.policy.Allow(kind, title) {
		answer, status = []acp.PermissionOptionKind{
			acp.PermissionOptionKindAllowOnce,
			acp.PermissionOptionKindAllowAlways,
		}, StatusAllowed
	}
	id, ok := pick(p.Options, answer)
	if !ok {
		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{Cancelled: &acp.RequestPermissionOutcomeCancelled{}},
		}, nil
	}
	c.emit(ctx, Event{Type: TypePermission, Tool: title, Kind: string(kind), Status: status})
	return acp.RequestPermissionResponse{
		Outcome: acp.RequestPermissionOutcome{Selected: &acp.RequestPermissionOutcomeSelected{OptionId: id}},
	}, nil
}

// pick returns the first offered option matching the preferred kinds, in kind
// order and then agent option order.
func pick(options []acp.PermissionOption, kinds []acp.PermissionOptionKind) (acp.PermissionOptionId, bool) {
	for _, kind := range kinds {
		for _, o := range options {
			if o.Kind == kind {
				return o.OptionId, true
			}
		}
	}
	return "", false
}

// emit queues an event until the request context ends. Cancellation prevents
// a full channel from blocking the client indefinitely.
func (c *client) emit(ctx context.Context, e Event) {
	select {
	case c.events <- e:
	case <-ctx.Done():
	}
}

// SessionUpdate converts supported updates to events in arrival order.
// Unknown update types are ignored for forward compatibility.
func (c *client) SessionUpdate(ctx context.Context, n acp.SessionNotification) error {
	switch u := n.Update; {
	case u.AgentMessageChunk != nil:
		c.emit(ctx, Event{Type: TypeText, Text: blockText(u.AgentMessageChunk.Content)})
	case u.AgentThoughtChunk != nil:
		c.emit(ctx, Event{Type: TypeThought, Text: blockText(u.AgentThoughtChunk.Content)})
	case u.ToolCall != nil:
		c.emit(ctx, Event{
			Type:    TypeTool,
			Tool:    u.ToolCall.Title,
			Summary: summary(u.ToolCall.Kind, u.ToolCall.Locations, u.ToolCall.RawInput),
			Kind:    string(u.ToolCall.Kind),
			Status:  string(u.ToolCall.Status),
		})
	case u.ToolCallUpdate != nil && u.ToolCallUpdate.Status != nil:
		c.emit(ctx, Event{Type: TypeToolUpdate, Status: string(*u.ToolCallUpdate.Status)})
	}
	return nil
}

// blockText extracts text content, returning empty for images and resources.
func blockText(b acp.ContentBlock) string {
	if b.Text == nil {
		return ""
	}
	return b.Text.Text
}

// summary describes a tool call using its command or first file location. Raw
// input fields vary by agent; only command is recognized here.
func summary(kind acp.ToolKind, locations []acp.ToolCallLocation, rawInput any) string {
	if kind == acp.ToolKindExecute {
		if input, ok := rawInput.(map[string]any); ok {
			if command, ok := input["command"].(string); ok {
				return oneLine(command)
			}
		}
	}
	if len(locations) > 0 {
		return locations[0].Path
	}
	return ""
}

// summaryWidth limits command summaries for terminal display.
const summaryWidth = 120

// oneLine returns the first line of s, truncating to summaryWidth characters
// with an ellipsis.
func oneLine(s string) string {
	first, rest, _ := strings.Cut(strings.TrimSpace(s), "\n")
	line := []rune(strings.TrimSpace(first))
	if len(line) <= summaryWidth && strings.TrimSpace(rest) == "" {
		return string(line)
	}
	if len(line) > summaryWidth-1 {
		line = line[:summaryWidth-1]
	}
	return string(line) + "…"
}

// Terminal requests are rejected because the client does not advertise
// terminal support. Agents must execute commands themselves.
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
