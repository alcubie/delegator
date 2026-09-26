package handler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	acp "github.com/coder/acp-go-sdk"
)

// serve builds a client for direct protocol-handler tests.
func serve() *client { return &client{} }

// option uses the permission kind as its ID for readable assertions.
func option(kind acp.PermissionOptionKind) acp.PermissionOption {
	return acp.PermissionOption{Kind: kind, Name: string(kind), OptionId: acp.PermissionOptionId(kind)}
}

// ask sends a permission request and returns the selected option ID and
// emitted events. No selection gives an empty ID.
func ask(t *testing.T, policy Policy, kind acp.ToolKind, options ...acp.PermissionOption) (string, []Event) {
	t.Helper()
	c := &client{policy: policy, events: make(chan Event, len(options)+1)}
	r, err := c.RequestPermission(t.Context(), acp.RequestPermissionRequest{
		ToolCall: acp.ToolCallUpdate{Kind: acp.Ptr(kind), Title: acp.Ptr("the tool's line")},
		Options:  options,
	})
	if err != nil {
		t.Fatalf("RequestPermission: %v", err)
	}
	if (r.Outcome.Selected == nil) == (r.Outcome.Cancelled == nil) {
		t.Fatalf("the outcome is %+v, and it has to be one of the two", r.Outcome)
	}
	close(c.events)
	var events []Event
	for e := range c.events {
		events = append(events, e)
	}
	if r.Outcome.Selected == nil {
		return "", events
	}
	return string(r.Outcome.Selected.OptionId), events
}

func TestReadTextFileServesAnAbsolutePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hello.txt")
	if err := os.WriteFile(path, []byte("what the agent asked for"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := serve().ReadTextFile(t.Context(), acp.ReadTextFileRequest{Path: path})
	if err != nil {
		t.Fatalf("ReadTextFile: %v", err)
	}
	if r.Content != "what the agent asked for" {
		t.Errorf("the content is %q", r.Content)
	}
}

func TestReadTextFileRefusesARelativePath(t *testing.T) {
	_, err := serve().ReadTextFile(t.Context(), acp.ReadTextFileRequest{Path: "hello.txt"})
	if err == nil {
		t.Fatal("a relative path was read")
	}
	if !strings.Contains(err.Error(), "hello.txt") || !strings.Contains(err.Error(), "absolute") {
		t.Errorf("the error is %v, and it should name the path and say what is wrong with it", err)
	}
}

func TestReadTextFileReportsAFileThatIsNotThere(t *testing.T) {
	_, err := serve().ReadTextFile(t.Context(), acp.ReadTextFileRequest{Path: filepath.Join(t.TempDir(), "gone.txt")})
	if err == nil {
		t.Fatal("a file that is not there was read")
	}
}

func TestWriteTextFileWritesThePathAndItsDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", "hello.txt")
	if _, err := serve().WriteTextFile(t.Context(), acp.WriteTextFileRequest{Path: path, Content: "written"}); err != nil {
		t.Fatalf("WriteTextFile: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the file was not written: %v", err)
	}
	if string(b) != "written" {
		t.Errorf("the file holds %q", b)
	}
}

func TestWriteTextFileRefusesARelativePath(t *testing.T) {
	_, err := serve().WriteTextFile(t.Context(), acp.WriteTextFileRequest{Path: "hello.txt", Content: "written"})
	if err == nil {
		t.Fatal("a relative path was written")
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Errorf("the error is %v, and it should say what is wrong with the path", err)
	}
}

func TestTheTerminalIsNotServed(t *testing.T) {
	c := serve()
	_, create := c.CreateTerminal(t.Context(), acp.CreateTerminalRequest{})
	_, output := c.TerminalOutput(t.Context(), acp.TerminalOutputRequest{})
	_, wait := c.WaitForTerminalExit(t.Context(), acp.WaitForTerminalExitRequest{})
	_, kill := c.KillTerminal(t.Context(), acp.KillTerminalRequest{})
	_, release := c.ReleaseTerminal(t.Context(), acp.ReleaseTerminalRequest{})
	for name, err := range map[string]error{"create": create, "output": output, "wait": wait, "kill": kill, "release": release} {
		if err == nil || !strings.Contains(err.Error(), "terminal not supported") {
			t.Errorf("terminal %s gave %v, and the client serves no terminal", name, err)
		}
	}
}

func TestRequestPermissionPrefersTheOptionThatAllowsOnce(t *testing.T) {
	got, events := ask(t, AllowAll(), acp.ToolKindEdit,
		option(acp.PermissionOptionKindAllowAlways),
		option(acp.PermissionOptionKindAllowOnce),
		option(acp.PermissionOptionKindRejectOnce))
	if want := string(acp.PermissionOptionKindAllowOnce); got != want {
		t.Errorf("the client selected %q, want %q: an answer for one tool call must not widen the next", got, want)
	}
	want := Event{Type: TypePermission, Tool: "the tool's line", Kind: string(acp.ToolKindEdit), Status: StatusAllowed}
	if len(events) != 1 || events[0] != want {
		t.Errorf("the client kept %+v, want the one event %+v", events, want)
	}
}

func TestRequestPermissionPrefersTheOptionThatRejectsOnce(t *testing.T) {
	got, events := ask(t, Deny(), acp.ToolKindExecute,
		option(acp.PermissionOptionKindRejectAlways),
		option(acp.PermissionOptionKindRejectOnce),
		option(acp.PermissionOptionKindAllowOnce))
	if want := string(acp.PermissionOptionKindRejectOnce); got != want {
		t.Errorf("the client selected %q, want %q", got, want)
	}
	want := Event{Type: TypePermission, Tool: "the tool's line", Kind: string(acp.ToolKindExecute), Status: StatusRejected}
	if len(events) != 1 || events[0] != want {
		t.Errorf("the client kept %+v, want the one event %+v", events, want)
	}
}

func TestRequestPermissionTakesTheOptionThatRejectsAlwaysWhenItIsTheOnlyOne(t *testing.T) {
	got, _ := ask(t, Deny(), acp.ToolKindExecute, option(acp.PermissionOptionKindRejectAlways))
	if want := string(acp.PermissionOptionKindRejectAlways); got != want {
		t.Errorf("the client selected %q, want %q: the policy said no, and the agent offered one way to say it", got, want)
	}
}

func TestRequestPermissionCancelsWhenNoOptionAnswersThePolicy(t *testing.T) {
	got, events := ask(t, AllowAll(), acp.ToolKindEdit, option(acp.PermissionOptionKindRejectOnce))
	if got != "" {
		t.Errorf("the client selected %q, and no option allows the tool", got)
	}
	if len(events) != 0 {
		t.Errorf("the client kept %+v, and it decided nothing", events)
	}
}

func TestRequestPermissionCancelsWhenTheAgentOffersNoOption(t *testing.T) {
	if got, _ := ask(t, AllowAll(), acp.ToolKindEdit); got != "" {
		t.Errorf("the client selected %q of no options", got)
	}
}

func TestAPolicyWithNoAllowRejects(t *testing.T) {
	got, _ := ask(t, Policy{}, acp.ToolKindRead,
		option(acp.PermissionOptionKindAllowOnce),
		option(acp.PermissionOptionKindRejectOnce))
	if want := string(acp.PermissionOptionKindRejectOnce); got != want {
		t.Errorf("the client selected %q, want %q: a policy that says nothing allows nothing", got, want)
	}
}

func TestAllowKindsAllowsOnlyTheKindsItNames(t *testing.T) {
	policy := AllowKinds(acp.ToolKindEdit, acp.ToolKindRead)
	for kind, allow := range map[acp.ToolKind]bool{
		acp.ToolKindEdit:    true,
		acp.ToolKindRead:    true,
		acp.ToolKindExecute: false,
		acp.ToolKindOther:   false,
	} {
		if got := policy.Allow(kind, "the tool's line"); got != allow {
			t.Errorf("the policy answers %v to a tool of kind %q, want %v", got, kind, allow)
		}
	}
}

func TestATitleAndAKindTheAgentLeavesOutReachThePolicy(t *testing.T) {
	var kind acp.ToolKind
	var title string
	seen := Policy{Allow: func(k acp.ToolKind, t string) bool { kind, title = k, t; return false }}
	c := &client{policy: seen, events: make(chan Event, 1)}
	if _, err := c.RequestPermission(t.Context(), acp.RequestPermissionRequest{
		Options: []acp.PermissionOption{option(acp.PermissionOptionKindRejectOnce)},
	}); err != nil {
		t.Fatalf("RequestPermission: %v", err)
	}
	if kind != acp.ToolKindOther || title != "" {
		t.Errorf("the policy was given kind %q and title %q, want %q and no title", kind, title, acp.ToolKindOther)
	}
}

func TestSessionUpdateDropsWhatIsNotAnEvent(t *testing.T) {
	c := &client{events: make(chan Event, 1)}
	plan := acp.UpdatePlan(acp.PlanEntry{Content: "run the tests", Status: acp.PlanEntryStatusPending})
	if err := c.SessionUpdate(t.Context(), acp.SessionNotification{Update: plan}); err != nil {
		t.Fatalf("SessionUpdate: %v", err)
	}
	if len(c.events) > 0 {
		t.Errorf("the client kept %+v of a plan, and a plan is not an event", <-c.events)
	}
}

func TestSummaryIsTheCommandOfAToolThatRunsOne(t *testing.T) {
	raw := map[string]any{"command": "make check", "description": "check it"}
	got := summary(acp.ToolKindExecute, []acp.ToolCallLocation{{Path: "/repo/Makefile"}}, raw)
	if got != "make check" {
		t.Errorf("the summary of a command is %q, and the agent ran %q", got, "make check")
	}
}

func TestSummaryIsTheFirstFileTheToolNames(t *testing.T) {
	locations := []acp.ToolCallLocation{{Path: "/repo/one.go"}, {Path: "/repo/two.go"}}
	if got := summary(acp.ToolKindRead, locations, nil); got != "/repo/one.go" {
		t.Errorf("the summary is %q, and the first file is %q", got, "/repo/one.go")
	}
	if got := summary(acp.ToolKindExecute, locations, map[string]any{}); got != "/repo/one.go" {
		t.Errorf("the summary of a command that raw input does not hold is %q, and the tool names %q", got, "/repo/one.go")
	}
	if got := summary(acp.ToolKindFetch, nil, nil); got != "" {
		t.Errorf("the summary of a tool that names no file is %q, and there is nothing to say", got)
	}
}

func TestSummaryIsOneLineOfAtMost120Characters(t *testing.T) {
	for _, c := range []struct{ name, command, want string }{
		{"one short line", "make check", "make check"},
		{"more than one line", "cd /repo\nmake check", "cd /repo…"},
		{"a line of over 120", strings.Repeat("a", 200), strings.Repeat("a", 119) + "…"},
		{"a line of 120", strings.Repeat("a", 120), strings.Repeat("a", 120)},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := summary(acp.ToolKindExecute, nil, map[string]any{"command": c.command})
			if got != c.want {
				t.Errorf("the summary is %q, want %q", got, c.want)
			}
			if n := len([]rune(got)); n > 120 {
				t.Errorf("the summary is %d characters, and a line is at most 120", n)
			}
		})
	}
}
