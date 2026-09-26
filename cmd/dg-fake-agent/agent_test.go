package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/handler"
	acp "github.com/coder/acp-go-sdk"
)

// selfArg makes a subprocess of this test binary serve as the fake agent,
// avoiding a separate build.
const selfArg = "-agent"

// Named script output makes event assertions readable.
const (
	fakeText     = "the work is done"
	fakeThought  = "the tests come first"
	fakeTitle    = "Read the run"
	fakeAsk      = "Edit the run"
	fakeContent  = "what the agent wrote"
	fakeSaid     = "the session said this before"
	fakeMeant    = "and then it thought this"
	fakeAnswered = "allow-once" // the option the handler's client takes
)

// TestMain selects agent mode before parsing test flags, since agent
// arguments are not testing flags.
func TestMain(m *testing.M) {
	if len(os.Args) == 3 && os.Args[1] == selfArg {
		if err := serve(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type agentLaunch struct {
	name string
	argv []string
}

// fakeLaunch writes the script and returns the agent command and permission-
// log path.
func fakeLaunch(t *testing.T, lines ...string) (agentLaunch, string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	path := filepath.Join(t.TempDir(), "script")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return agentLaunch{name: "fake", argv: []string{self, selfArg, path}}, path + recordSuffix
}

// startFake starts the agent on the script in cwd and closes it when the test
// ends.
func startFake(t *testing.T, cwd string, agent agentLaunch) *handler.Session {
	t.Helper()
	s, err := handler.Start(t.Context(), agent.name, agent.argv, handler.AllowAll(), cwd, handler.SessionOptions{}, io.Discard)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// turn runs one prompt to the end of its sequence and gives the events in
// order.
func turn(t *testing.T, s *handler.Session, text string) []handler.Event {
	t.Helper()
	var got []handler.Event
	var bad error
	for e, err := range s.Prompt(t.Context(), text) {
		got = append(got, e)
		if err != nil {
			bad = err
		}
	}
	if bad != nil {
		t.Fatalf("the prompt failed: %v", bad)
	}
	return got
}

// same fails unless the turn gave the events wanted, in that order.
func same(t *testing.T, got, want []handler.Event) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("the turn gave\n %+v\nwant\n %+v", got, want)
	}
}

// everyAction exercises script actions and checks events and file writes.
// Permission comes first because requests and notifications can be dispatched
// out of order; waiting for its response orders subsequent updates.
func everyAction(t *testing.T) (agent agentLaunch, record, written string, want []handler.Event) {
	t.Helper()
	dir := t.TempDir()
	read := filepath.Join(dir, "run.go")
	written = filepath.Join(dir, "reports", "done.md")
	agent, record = fakeLaunch(t,
		"permission "+fakeAsk+" edit",
		"text "+fakeText,
		"thought "+fakeThought,
		"tool read "+fakeTitle+" "+read,
		"update in_progress",
		"write "+written+" "+fakeContent,
		"stop end_turn",
	)
	return agent, record, written, []handler.Event{
		{
			Type:   handler.TypePermission,
			Tool:   fakeAsk,
			Kind:   string(acp.ToolKindEdit),
			Status: handler.StatusAllowed,
		},
		{Type: handler.TypeText, Text: fakeText},
		{Type: handler.TypeThought, Text: fakeThought},
		{
			Type:    handler.TypeTool,
			Tool:    fakeTitle,
			Summary: read,
			Kind:    string(acp.ToolKindRead),
			Status:  string(acp.ToolCallStatusPending),
		},
		{Type: handler.TypeToolUpdate, Status: string(acp.ToolCallStatusInProgress)},
		{Type: handler.TypeResult, Status: string(acp.StopReasonEndTurn)},
	}
}

func TestTheAgentDoesEveryActionOfAScript(t *testing.T) {
	agent, _, _, want := everyAction(t)
	same(t, turn(t, startFake(t, t.TempDir(), agent), "do the work"), want)
}

func TestTheAgentReturnsStandardPromptUsageFromAScript(t *testing.T) {
	agent, _ := fakeLaunch(t,
		`usage {"inputTokens":11,"cachedWriteTokens":2,"cachedReadTokens":3,"outputTokens":5,"thoughtTokens":4,"totalTokens":21}`,
		"stop end_turn",
	)
	s := startFake(t, t.TempDir(), agent)
	got := turn(t, s, "do the work")
	if len(got) != 1 || got[0].Type != handler.TypeResult {
		t.Fatalf("the turn gave %+v, want one result", got)
	}
	want := &handler.Usage{
		InputTokens:       11,
		CachedWriteTokens: intPointer(2),
		CachedReadTokens:  intPointer(3),
		OutputTokens:      5,
		ThoughtTokens:     intPointer(4),
		TotalTokens:       21,
	}
	if usage := s.Usage(); !reflect.DeepEqual(usage, want) {
		t.Errorf("usage = %+v, want %+v", usage, want)
	}
}

func intPointer(n int) *int { return &n }

func TestTheWriteActionGoesThroughTheClient(t *testing.T) {
	agent, _, written, _ := everyAction(t)
	turn(t, startFake(t, t.TempDir(), agent), "do the work")
	content, err := os.ReadFile(written)
	if err != nil {
		t.Fatalf("the agent wrote no file: %v", err)
	}
	if got := string(content); got != fakeContent {
		t.Errorf("the file holds %q, and the script said %q", got, fakeContent)
	}
}

func TestThePromptActionRecordsThePrompt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prompt")
	agent, _ := fakeLaunch(t, "prompt "+path, "stop end_turn")
	turn(t, startFake(t, t.TempDir(), agent), "do the work")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the agent recorded no prompt: %v", err)
	}
	if string(got) != "do the work" {
		t.Errorf("the prompt record holds %q, want %q", got, "do the work")
	}
}

func TestThePermissionActionRecordsTheAnswer(t *testing.T) {
	agent, record, _, _ := everyAction(t)
	turn(t, startFake(t, t.TempDir(), agent), "do the work")
	got, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("the agent recorded nothing: %v", err)
	}
	if want := fakeAnswered + "\n"; string(got) != want {
		t.Errorf("the record holds %q, want %q", got, want)
	}
}

func TestTheAgentIssuesTheSessionItCounted(t *testing.T) {
	agent, _ := fakeLaunch(t, "stop end_turn")
	if got := startFake(t, t.TempDir(), agent).ID(); got != "fake-1" {
		t.Errorf("the agent issued %q, want %q", got, "fake-1")
	}
}

func TestASecondPromptRunsTheScriptAgain(t *testing.T) {
	agent, _, _, want := everyAction(t)
	s := startFake(t, t.TempDir(), agent)
	turn(t, s, "the first prompt")
	same(t, turn(t, s, "the second prompt"), want)
}

func TestTheStopActionGivesTheReasonTheScriptNamed(t *testing.T) {
	agent, _ := fakeLaunch(t, "stop refusal")
	same(t, turn(t, startFake(t, t.TempDir(), agent), "do the work"),
		[]handler.Event{{Type: handler.TypeResult, Status: string(acp.StopReasonRefusal)}})
}

func TestAScriptThatNamesNoReasonEndsTheTurn(t *testing.T) {
	agent, _ := fakeLaunch(t, "text "+fakeText)
	same(t, turn(t, startFake(t, t.TempDir(), agent), "do the work"), []handler.Event{
		{Type: handler.TypeText, Text: fakeText},
		{Type: handler.TypeResult, Status: string(acp.StopReasonEndTurn)},
	})
}

func TestTheHistorySectionIsReplayedOnLoad(t *testing.T) {
	dir := t.TempDir()
	agent, _ := fakeLaunch(t,
		"text "+fakeText,
		"stop end_turn",
		"history:",
		"text "+fakeSaid,
		"thought "+fakeMeant,
	)
	s, err := handler.Load(t.Context(), agent.name, agent.argv, handler.AllowAll(), dir, "fake-1", handler.SessionOptions{}, io.Discard)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	same(t, turn(t, s, "do the work"), []handler.Event{
		{Type: handler.TypeText, Text: fakeSaid, Replay: true},
		{Type: handler.TypeThought, Text: fakeMeant, Replay: true},
		{Type: handler.TypeText, Text: fakeText},
		{Type: handler.TypeResult, Status: string(acp.StopReasonEndTurn)},
	})
}

func TestAnActionTheScriptHasNoVerbForFailsTheTurn(t *testing.T) {
	agent, _ := fakeLaunch(t, "frobnicate the run")
	s := startFake(t, t.TempDir(), agent)
	var got []handler.Event
	var bad error
	for e, err := range s.Prompt(t.Context(), "do the work") {
		got = append(got, e)
		if err != nil {
			bad = err
		}
	}
	if bad == nil {
		t.Fatal("a script of an action that is not there took the turn")
	}
	if len(got) != 1 || got[0].Type != handler.TypeError {
		t.Fatalf("the turn gave %+v, want one error event", got)
	}
	if !strings.Contains(got[0].Err, "frobnicate") {
		t.Errorf("the error is %q, and it does not name the action", got[0].Err)
	}
}

// Wait gives the client time to cancel an active turn.
func TestTheWaitActionEndsTheTurnWhenTheClientCancels(t *testing.T) {
	agent, _ := fakeLaunch(t, "text "+fakeText, "wait 1m", "stop end_turn")
	s := startFake(t, t.TempDir(), agent)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var got []handler.Event
	for e, err := range s.Prompt(ctx, "do the work") {
		if err != nil {
			t.Fatalf("the prompt failed: %v", err)
		}
		got = append(got, e)
		cancel()
	}

	same(t, got, []handler.Event{
		{Type: handler.TypeText, Text: fakeText},
		{Type: handler.TypeResult, Status: string(acp.StopReasonCancelled)},
	})
}

func TestTheWaitActionRefusesATimeItCannotRead(t *testing.T) {
	agent, _ := fakeLaunch(t, "wait soon")
	var bad error
	for _, err := range startFake(t, t.TempDir(), agent).Prompt(t.Context(), "do the work") {
		if err != nil {
			bad = err
		}
	}
	if bad == nil || !strings.Contains(bad.Error(), "soon") {
		t.Fatalf("the error is %v, and it does not name the time", bad)
	}
}

// Continue ignores ACP cancellation so the test must rely on supervisor
// process termination.
func TestTheContinueActionRunsPastCancellation(t *testing.T) {
	kind, _ := fakeLaunch(t, "continue 100ms", "stop end_turn")
	s := startFake(t, t.TempDir(), kind)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()

	got := turnWithContext(t, ctx, s)

	if elapsed := time.Since(started); elapsed < 80*time.Millisecond {
		t.Errorf("the action ran for %v, want it to continue past cancellation", elapsed)
	}
	same(t, got, []handler.Event{{Type: handler.TypeResult, Status: string(acp.StopReasonEndTurn)}})
}

func turnWithContext(t *testing.T, ctx context.Context, s *handler.Session) []handler.Event {
	t.Helper()
	var got []handler.Event
	for e, err := range s.Prompt(ctx, "do the work") {
		if err != nil {
			t.Fatalf("the prompt failed: %v", err)
		}
		got = append(got, e)
	}
	return got
}
