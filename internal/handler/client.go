package handler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	acp "github.com/coder/acp-go-sdk"
)

// client answers what an agent asks of delegator: the files it reads and
// writes, the permission it asks for a tool, and the updates it sends as a
// turn goes on. What it decides on its own it puts onto events, so the run
// reads the decision that was made for it.
type client struct {
	policy Policy
	events chan Event
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

// RequestPermission answers the agent by the policy. It takes the option that
// answers once before the one that answers always, so the answer to one tool
// call never widens the next, and it records the decision as an event.
//
// An agent that offers no option of the answer the policy gives is told the
// request was cancelled, which is the protocol's way of saying that no option
// was taken. Nothing was decided, so nothing is recorded.
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

// pick gives the id of the first option the agent offered of the first kind
// that it has, so the order of the kinds is the order of the preference and
// the order of the options is the agent's.
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

// emit keeps an event for the drain to take. It gives up when the request is
// over, so an agent that asks more than the channel holds while nothing reads
// it stops the turn rather than the process.
func (c *client) emit(ctx context.Context, e Event) {
	select {
	case c.events <- e:
	case <-ctx.Done():
	}
}

// SessionUpdate turns what the agent reports of the turn into an event and
// keeps it for the drain, in the order the agent sent it. An update of a kind
// the Event type has no room for is dropped: a client that fails on an update
// it does not know would stop the turn every time an agent grew one.
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

// blockText is the text of a block of content. A block of an image or of a
// resource has none, and reports nothing rather than a line about itself.
func blockText(b acp.ContentBlock) string {
	if b.Text == nil {
		return ""
	}
	return b.Text.Text
}

// summary is the one line a tool event shows of what the tool was given: the
// command of a tool that runs one, and the first file of a tool that names
// one. The raw input is the tool's own arguments, which every agent names
// differently but for the command.
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

// summaryWidth is how many characters of a command a summary shows. A run is
// read in a terminal beside the text around it, and a command of three
// hundred characters would take the screen from it.
const summaryWidth = 120

// oneLine is the first line of s, cut to summaryWidth characters. What is cut
// ends in an ellipsis, so a reader knows that the line is not all there was.
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
