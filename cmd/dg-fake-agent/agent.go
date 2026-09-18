package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

// recordSuffix names the file of permission answers, beside the script.
const recordSuffix = ".record"

// recordPerm is the permission of the record file, which is the agent's own
// working file in a test's directory and not delegator's data.
const recordPerm = 0o644

// options is what the agent offers for every permission it asks for: one of
// each kind, so the answer says which kind the client took as well as that it
// answered.
var options = []acp.PermissionOption{
	{Kind: acp.PermissionOptionKindAllowAlways, Name: "Always allow", OptionId: "allow-always"},
	{Kind: acp.PermissionOptionKindAllowOnce, Name: "Allow", OptionId: "allow-once"},
	{Kind: acp.PermissionOptionKindRejectAlways, Name: "Always reject", OptionId: "reject-always"},
	{Kind: acp.PermissionOptionKindRejectOnce, Name: "Reject", OptionId: "reject-once"},
}

// A fakeAgent serves the agent side of the protocol by running a script.
type fakeAgent struct {
	script   script
	record   string
	conn     *acp.AgentSideConnection
	sessions int
	tools    int
	asks     int
	answers  []string
}

var (
	_ acp.Agent       = (*fakeAgent)(nil)
	_ acp.AgentLoader = (*fakeAgent)(nil)
)

func (a *fakeAgent) Initialize(context.Context, acp.InitializeRequest) (acp.InitializeResponse, error) {
	return acp.InitializeResponse{
		ProtocolVersion:   acp.ProtocolVersionNumber,
		AgentCapabilities: acp.AgentCapabilities{LoadSession: true},
	}, nil
}

// NewSession issues the next id of this process.
func (a *fakeAgent) NewSession(context.Context, acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	a.sessions++
	return acp.NewSessionResponse{SessionId: acp.SessionId("fake-" + strconv.Itoa(a.sessions))}, nil
}

// LoadSession replays the history and then answers, which is the order the
// protocol asks for. It takes whatever id it is given: the process that issued
// the id was another one and this one holds no record of it, so an id it
// refused would be every id a test could load.
func (a *fakeAgent) LoadSession(ctx context.Context, p acp.LoadSessionRequest) (acp.LoadSessionResponse, error) {
	_, _, err := a.take(ctx, p.SessionId, a.script.history)
	return acp.LoadSessionResponse{}, err
}

// Prompt runs the turn of the script. Every prompt runs it again, so a test
// that takes two turns writes the turn once.
func (a *fakeAgent) Prompt(ctx context.Context, p acp.PromptRequest) (acp.PromptResponse, error) {
	stop, usage, err := a.take(ctx, p.SessionId, a.script.turn)
	if err != nil {
		return acp.PromptResponse{}, err
	}
	return acp.PromptResponse{StopReason: stop, Usage: usage}, nil
}

// take does the actions in order and gives the reason and standard aggregate
// usage the turn reports. A usage action is the JSON object ACP puts in
// PromptResponse. A run of actions that names no reason ends the turn, which
// is what an agent that has run out of things to do reports.
func (a *fakeAgent) take(ctx context.Context, id acp.SessionId, actions []string) (acp.StopReason, *acp.Usage, error) {
	var usage *acp.Usage
	for _, action := range actions {
		verb, rest, _ := strings.Cut(action, " ")
		rest = strings.TrimSpace(rest)
		var err error
		switch verb {
		case "text":
			err = a.update(ctx, id, acp.UpdateAgentMessageText(rest))
		case "thought":
			err = a.update(ctx, id, acp.UpdateAgentThoughtText(rest))
		case "tool":
			err = a.tool(ctx, id, rest)
		case "update":
			err = a.update(ctx, id, acp.UpdateToolCall(a.toolID(), acp.WithUpdateStatus(acp.ToolCallStatus(rest))))
		case "permission":
			err = a.permission(ctx, id, rest)
		case "write":
			path, content, _ := strings.Cut(rest, " ")
			_, err = a.conn.WriteTextFile(ctx, acp.WriteTextFileRequest{SessionId: id, Path: path, Content: content})
		case "wait":
			cancelled, bad := wait(ctx, rest)
			if cancelled {
				return acp.StopReasonCancelled, usage, nil
			}
			err = bad
		case "continue":
			err = continueFor(rest)
		case "usage":
			var reported acp.Usage
			if err = json.Unmarshal([]byte(rest), &reported); err == nil {
				usage = &reported
			}
		case "stop":
			return acp.StopReason(rest), usage, nil
		default:
			err = fmt.Errorf("the script has no action %q", verb)
		}
		if err != nil {
			return "", nil, err
		}
	}
	return acp.StopReasonEndTurn, usage, nil
}

// continueFor waits without consulting the turn's context. It represents an
// agent or a program below it that only the supervisor's process stop ends.
func continueFor(rest string) error {
	d, err := time.ParseDuration(rest)
	if err != nil {
		return err
	}
	time.Sleep(d)
	return nil
}

// wait holds the turn for the time the line names and says whether the client
// cancelled before that time was up.
func wait(ctx context.Context, rest string) (cancelled bool, err error) {
	d, err := time.ParseDuration(rest)
	if err != nil {
		return false, err
	}
	select {
	case <-ctx.Done():
		return true, nil
	case <-time.After(d):
		return false, nil
	}
}

// update sends one session update to the client.
func (a *fakeAgent) update(ctx context.Context, id acp.SessionId, u acp.SessionUpdate) error {
	return a.conn.SessionUpdate(ctx, acp.SessionNotification{SessionId: id, Update: u})
}

// tool starts a tool call of a kind and a title, on the file the line names.
// A title holds spaces and a path does not, so the last word is the path when
// it is absolute, which is what the protocol asks of a location, and the end
// of the title when it is not.
func (a *fakeAgent) tool(ctx context.Context, id acp.SessionId, rest string) error {
	kind, title, _ := strings.Cut(rest, " ")
	opts := []acp.ToolCallStartOpt{
		acp.WithStartKind(acp.ToolKind(kind)),
		acp.WithStartStatus(acp.ToolCallStatusPending),
	}
	if head, last, ok := cutLast(title); ok && filepath.IsAbs(last) {
		title = head
		opts = append(opts, acp.WithStartLocations([]acp.ToolCallLocation{{Path: last}}))
	}
	a.tools++
	return a.update(ctx, id, acp.StartToolCall(a.toolID(), title, opts...))
}

// permission asks to run a tool of a title and a kind, and records the option
// the client chose.
func (a *fakeAgent) permission(ctx context.Context, id acp.SessionId, rest string) error {
	title, kind, ok := cutLast(rest)
	if !ok {
		return fmt.Errorf("permission takes a title and a kind, and the line is %q", rest)
	}
	a.asks++
	r, err := a.conn.RequestPermission(ctx, acp.RequestPermissionRequest{
		SessionId: id,
		ToolCall: acp.ToolCallUpdate{
			ToolCallId: acp.ToolCallId("ask-" + strconv.Itoa(a.asks)),
			Kind:       acp.Ptr(acp.ToolKind(kind)),
			Title:      acp.Ptr(title),
		},
		Options: options,
	})
	if err != nil {
		return err
	}
	answer := "cancelled"
	if r.Outcome.Selected != nil {
		answer = string(r.Outcome.Selected.OptionId)
	}
	a.answers = append(a.answers, answer)
	return os.WriteFile(a.record, []byte(strings.Join(a.answers, "\n")+"\n"), recordPerm)
}

// toolID names the tool call the script started last, which update reports
// the status of.
func (a *fakeAgent) toolID() acp.ToolCallId {
	return acp.ToolCallId("call-" + strconv.Itoa(a.tools))
}

// cutLast cuts s at its last space.
func cutLast(s string) (head, last string, ok bool) {
	i := strings.LastIndex(s, " ")
	if i < 0 {
		return s, "", false
	}
	return s[:i], s[i+1:], true
}

// The rest of the agent side is not what a script says, so each method gives
// the empty answer of its call.
func (a *fakeAgent) Authenticate(context.Context, acp.AuthenticateRequest) (acp.AuthenticateResponse, error) {
	return acp.AuthenticateResponse{}, nil
}
func (a *fakeAgent) Logout(context.Context, acp.LogoutRequest) (acp.LogoutResponse, error) {
	return acp.LogoutResponse{}, nil
}
func (a *fakeAgent) Cancel(context.Context, acp.CancelNotification) error { return nil }
func (a *fakeAgent) CloseSession(context.Context, acp.CloseSessionRequest) (acp.CloseSessionResponse, error) {
	return acp.CloseSessionResponse{}, nil
}
func (a *fakeAgent) ListSessions(context.Context, acp.ListSessionsRequest) (acp.ListSessionsResponse, error) {
	return acp.ListSessionsResponse{}, nil
}
func (a *fakeAgent) ResumeSession(context.Context, acp.ResumeSessionRequest) (acp.ResumeSessionResponse, error) {
	return acp.ResumeSessionResponse{}, nil
}
func (a *fakeAgent) SetSessionConfigOption(context.Context, acp.SetSessionConfigOptionRequest) (acp.SetSessionConfigOptionResponse, error) {
	return acp.SetSessionConfigOptionResponse{}, nil
}
func (a *fakeAgent) SetSessionMode(context.Context, acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	return acp.SetSessionModeResponse{}, nil
}
