package handler

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"

	acp "github.com/coder/acp-go-sdk"
)

// startTurn opens a session on the stub agent taking one of its turns, under
// the policy that allows every tool.
func startTurn(t *testing.T, turn string) *Session {
	t.Helper()
	s, _, _ := startPolicy(t, t.TempDir(), AllowAll(), stubFullOptions, turn)
	return s
}

// turn runs one prompt to the end of its sequence and gives back the
// events in order and the error the sequence ended with, if it ended with one.
func turn(ctx context.Context, s *Session, text string) ([]Event, error) {
	var events []Event
	var bad error
	for e, err := range s.Prompt(ctx, text) {
		events = append(events, e)
		if err != nil {
			bad = err
		}
	}
	return events, bad
}

// same fails unless the prompt gave the events wanted, in that order.
func same(t *testing.T, got, want []Event) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("the prompt gave\n %+v\nwant\n %+v", got, want)
	}
}

// updateEvents is the sequence the updates turn makes of a prompt of text.
func updateEvents(text string) []Event {
	return []Event{
		{Type: TypeText, Text: text},
		{Type: TypeThought, Text: stubThought},
		{
			Type:    TypeTool,
			Tool:    stubReadTitle,
			Summary: stubReadPath,
			Kind:    string(acp.ToolKindRead),
			Status:  string(acp.ToolCallStatusPending),
		},
		{Type: TypeToolUpdate, Status: string(acp.ToolCallStatusInProgress)},
		{
			Type:    TypeTool,
			Tool:    stubExecuteTitle,
			Summary: stubCommandLine,
			Kind:    string(acp.ToolKindExecute),
			Status:  string(acp.ToolCallStatusPending),
		},
		{Type: TypeResult, Status: string(acp.StopReasonEndTurn)},
	}
}

func TestPromptGivesAnEventForEveryUpdateAndEndsWithTheStopReason(t *testing.T) {
	got, err := turn(t.Context(), startTurn(t, stubTurnUpdates), "do the work")
	if err != nil {
		t.Fatalf("the prompt failed: %v", err)
	}
	same(t, got, updateEvents("do the work"))
}

func TestPromptCarriesThePermissionsThePolicyAnswered(t *testing.T) {
	got, err := turn(t.Context(), startTurn(t, stubTurnPermissions), "do the work")
	if err != nil {
		t.Fatalf("the prompt failed: %v", err)
	}
	same(t, got, []Event{
		{Type: TypePermission, Tool: stubEditTitle, Kind: string(acp.ToolKindEdit), Status: StatusAllowed},
		{Type: TypePermission, Tool: stubExecuteTitle, Kind: string(acp.ToolKindExecute), Status: StatusAllowed},
		{Type: TypeResult, Status: string(acp.StopReasonEndTurn)},
	})
}

func TestPromptCancelledInTheTurnEndsAsCancelled(t *testing.T) {
	s := startTurn(t, stubTurnHang)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var got []Event
	for e, err := range s.Prompt(ctx, "do the work") {
		if err != nil {
			t.Fatalf("the prompt failed: %v", err)
		}
		got = append(got, e)
		cancel()
	}
	same(t, got, []Event{
		{Type: TypeText, Text: "do the work"},
		{Type: TypeResult, Status: string(acp.StopReasonCancelled)},
	})
}

func TestPromptEndsWithTheErrorTheAgentGave(t *testing.T) {
	got, err := turn(t.Context(), startTurn(t, stubTurnError), "do the work")
	if err == nil {
		t.Fatal("the prompt of an agent that failed gave no error")
	}
	if len(got) != 1 || got[0].Type != TypeError {
		t.Fatalf("the prompt gave %+v, want one error event", got)
	}
	if !strings.Contains(got[0].Err, stubFailure) || !strings.Contains(err.Error(), stubFailure) {
		t.Errorf("the event is %+v and the error is %v, and neither says %q", got[0], err, stubFailure)
	}
}

func TestTheEndOfATurnGivesWhatTheAgentSentLastBeforeTheResult(t *testing.T) {
	s := &Session{events: make(chan Event, 2)}
	s.events <- Event{Type: TypeText, Text: "the last word"}
	var got []Event
	s.end(outcome{stop: acp.StopReasonEndTurn}, func(e Event, _ error) bool {
		got = append(got, e)
		return true
	})
	same(t, got, []Event{
		{Type: TypeText, Text: "the last word"},
		{Type: TypeResult, Status: string(acp.StopReasonEndTurn)},
	})
}

func TestATurnTheCallerAbandonedLeavesNothingForTheNextOne(t *testing.T) {
	s := &Session{events: make(chan Event, eventRoom)}
	done := make(chan outcome, 1)
	for range eventRoom {
		s.events <- Event{Type: TypeText, Text: "sent while the turn was stopping"}
	}
	done <- outcome{stop: acp.StopReasonCancelled}
	s.abandon(done)
	if len(s.events) > 0 {
		t.Errorf("the turn left %d events for the next turn, and it takes them all", len(s.events))
	}
}

func TestASessionTakesAPromptAfterACallerStoppedReading(t *testing.T) {
	s := startTurn(t, stubTurnUpdates)
	for range s.Prompt(t.Context(), "the first prompt") {
		break
	}
	got, err := turn(t.Context(), s, "the second prompt")
	if err != nil {
		t.Fatalf("the prompt failed: %v", err)
	}
	same(t, got, updateEvents("the second prompt"))
}

func TestTwoPromptsOfOneSessionRunOneAfterTheOther(t *testing.T) {
	s := startTurn(t, stubTurnUpdates)
	var wg sync.WaitGroup
	for _, text := range []string{"the first prompt", "the second prompt"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := turn(t.Context(), s, text)
			if err != nil {
				t.Errorf("the prompt %q failed: %v", text, err)
				return
			}
			same(t, got, updateEvents(text))
		}()
	}
	wg.Wait()
}
