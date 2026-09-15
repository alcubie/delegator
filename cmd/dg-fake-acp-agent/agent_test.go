package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/handler"
	acp "github.com/coder/acp-go-sdk"
)

// selfArg is the first argument that tells a copy of the test binary to be the
// agent rather than the tests. A test starts the agent as a second copy of
// itself, so the exchange runs against this code and needs no build.
const selfArg = "-agent"

// What the scripts of the tests say, named so that a test reads what it wrote
// back out of the events.
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

// TestMain is the agent when the arguments say so, and the tests when they do
// not. It answers before the testing flags are parsed, because the arguments
// of the agent are not flags of a test.
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

// fakeKind writes the script to a file and gives the Kind that starts the
// agent on it and the path the agent records its permission answers in.
func fakeKind(t *testing.T, lines ...string) (handler.Kind, string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	path := filepath.Join(t.TempDir(), "script")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return handler.Kind{Name: "fake", Argv: []string{self, selfArg, path}}, path + recordSuffix
}

// startFake starts the agent on the script in cwd and closes it when the test
// ends.
func startFake(t *testing.T, cwd string, kind handler.Kind) *handler.Session {
	t.Helper()
	s, err := handler.Start(t.Context(), kind, handler.AllowAll(), cwd, io.Discard)
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

// everyAction is a script of one line of each action, and the events and the
// file that the handler's client makes of it.
//
// The permission comes first because it is a request and the rest are
// notifications: the agent waits for the answer, so everything after it
// reaches the client after it, while a request sent after a notification can
// be seen before it.
func everyAction(t *testing.T) (kind handler.Kind, record, written string, want []handler.Event) {
	t.Helper()
	dir := t.TempDir()
	read := filepath.Join(dir, "run.go")
	written = filepath.Join(dir, "reports", "done.md")
	kind, record = fakeKind(t,
		"permission "+fakeAsk+" edit",
		"text "+fakeText,
		"thought "+fakeThought,
		"tool read "+fakeTitle+" "+read,
		"update in_progress",
		"write "+written+" "+fakeContent,
		"stop end_turn",
	)
	return kind, record, written, []handler.Event{
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
	kind, _, _, want := everyAction(t)
	same(t, turn(t, startFake(t, t.TempDir(), kind), "do the work"), want)
}

func TestTheWriteActionGoesThroughTheClient(t *testing.T) {
	kind, _, written, _ := everyAction(t)
	turn(t, startFake(t, t.TempDir(), kind), "do the work")
	content, err := os.ReadFile(written)
	if err != nil {
		t.Fatalf("the agent wrote no file: %v", err)
	}
	if got := string(content); got != fakeContent {
		t.Errorf("the file holds %q, and the script said %q", got, fakeContent)
	}
}

func TestThePermissionActionRecordsTheAnswer(t *testing.T) {
	kind, record, _, _ := everyAction(t)
	turn(t, startFake(t, t.TempDir(), kind), "do the work")
	got, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("the agent recorded nothing: %v", err)
	}
	if want := fakeAnswered + "\n"; string(got) != want {
		t.Errorf("the record holds %q, want %q", got, want)
	}
}

func TestTheAgentIssuesTheSessionItCounted(t *testing.T) {
	kind, _ := fakeKind(t, "stop end_turn")
	if got := startFake(t, t.TempDir(), kind).ID(); got != "fake-1" {
		t.Errorf("the agent issued %q, want %q", got, "fake-1")
	}
}

func TestASecondPromptRunsTheScriptAgain(t *testing.T) {
	kind, _, _, want := everyAction(t)
	s := startFake(t, t.TempDir(), kind)
	turn(t, s, "the first prompt")
	same(t, turn(t, s, "the second prompt"), want)
}

func TestTheStopActionGivesTheReasonTheScriptNamed(t *testing.T) {
	kind, _ := fakeKind(t, "stop refusal")
	same(t, turn(t, startFake(t, t.TempDir(), kind), "do the work"),
		[]handler.Event{{Type: handler.TypeResult, Status: string(acp.StopReasonRefusal)}})
}

func TestAScriptThatNamesNoReasonEndsTheTurn(t *testing.T) {
	kind, _ := fakeKind(t, "text "+fakeText)
	same(t, turn(t, startFake(t, t.TempDir(), kind), "do the work"), []handler.Event{
		{Type: handler.TypeText, Text: fakeText},
		{Type: handler.TypeResult, Status: string(acp.StopReasonEndTurn)},
	})
}

func TestTheHistorySectionIsReplayedOnLoad(t *testing.T) {
	dir := t.TempDir()
	kind, _ := fakeKind(t,
		"text "+fakeText,
		"stop end_turn",
		"history:",
		"text "+fakeSaid,
		"thought "+fakeMeant,
	)
	s, err := handler.Load(t.Context(), kind, handler.AllowAll(), dir, "fake-1", io.Discard)
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
	kind, _ := fakeKind(t, "frobnicate the run")
	s := startFake(t, t.TempDir(), kind)
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

// The wait action is how a test drives a client that stops a turn: nothing
// else in a script takes long enough for the client to reach for the cancel.
func TestTheWaitActionEndsTheTurnWhenTheClientCancels(t *testing.T) {
	kind, _ := fakeKind(t, "text "+fakeText, "wait 1m", "stop end_turn")
	s := startFake(t, t.TempDir(), kind)
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
	kind, _ := fakeKind(t, "wait soon")
	var bad error
	for _, err := range startFake(t, t.TempDir(), kind).Prompt(t.Context(), "do the work") {
		if err != nil {
			bad = err
		}
	}
	if bad == nil || !strings.Contains(bad.Error(), "soon") {
		t.Fatalf("the error is %v, and it does not name the time", bad)
	}
}
