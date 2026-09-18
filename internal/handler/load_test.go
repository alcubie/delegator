package handler

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// loadSession loads the session the id names from a stub agent that says
// whether it can load one, and closes it when the test ends. It gives back
// the path of the file the agent records into, so a test that expects the
// load to be refused sees that the agent was never asked.
func loadSession(t *testing.T, id, loading string) (*Session, string, error) {
	return loadSessionConfigured(t, id, loading, SessionOptions{})
}

func loadSessionConfigured(t *testing.T, id, loading string, options SessionOptions) (*Session, string, error) {
	t.Helper()
	name, argv, record := stubLaunch(t, stubFullOptions, stubTurnUpdates, loading)
	// A load that never returns is the failure this bounds: the history can be
	// longer than the channel between the client and a turn, and nothing
	// drains that channel until the first prompt.
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	s, err := Load(ctx, name, argv, AllowAll(), t.TempDir(), id, options, io.Discard)
	if s != nil {
		t.Cleanup(func() { _ = s.Close() })
	}
	return s, record, err
}

func TestLoadPassesAdditionalDirectoriesAndEnvironment(t *testing.T) {
	directories := []string{filepath.Join(t.TempDir(), "project cache")}
	t.Setenv(stubEnvironmentName, "the inherited value")
	_, record, err := loadSessionConfigured(t, stubSessionID, stubLoads, SessionOptions{
		AdditionalDirectories: directories,
		Environment:           []string{stubEnvironmentName + "=the session value"},
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	r := readRecord(t, record)
	if !slices.Equal(r.AdditionalDirectories, directories) {
		t.Errorf("additional directories = %v, want %v", r.AdditionalDirectories, directories)
	}
	if r.Environment != "the session value" {
		t.Errorf("environment = %q, want the session value", r.Environment)
	}
}

func TestLoadOpensTheSessionTheCallerNamed(t *testing.T) {
	s, _, err := loadSession(t, stubSessionID, stubLoads)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.ID() != stubSessionID {
		t.Errorf("ID() = %q, and the caller named %q", s.ID(), stubSessionID)
	}
	if !s.Loaded() {
		t.Error("the session says it was not loaded, and Load opened it")
	}
}

func TestAStartedSessionWasNotLoaded(t *testing.T) {
	s, _, _ := start(t, t.TempDir())
	if s.Loaded() {
		t.Error("the session says it was loaded, and Start opened a new one")
	}
}

func TestLoadGivesTheHistoryAsReplayBeforeTheTurn(t *testing.T) {
	s, _, err := loadSession(t, stubSessionID, stubLoads)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, err := turn(t.Context(), s, "do the work")
	if err != nil {
		t.Fatalf("the prompt failed: %v", err)
	}
	want := []Event{
		{Type: TypeText, Text: stubReplayFirst, Replay: true},
		{Type: TypeText, Text: stubReplaySecond, Replay: true},
	}
	same(t, got, append(want, updateEvents("do the work")...))
}

func TestLoadGivesAHistoryLongerThanTheChannelHolds(t *testing.T) {
	s, _, err := loadSession(t, stubSessionID, stubLoadsLong)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, err := turn(t.Context(), s, "do the work")
	if err != nil {
		t.Fatalf("the prompt failed: %v", err)
	}
	want := make([]Event, 0, stubLongHistory)
	for i := range stubLongHistory {
		want = append(want, Event{Type: TypeText, Text: stubHistoryLine(i), Replay: true})
	}
	same(t, got, append(want, updateEvents("do the work")...))
}

func TestTheSecondTurnOfALoadedSessionHasNoHistory(t *testing.T) {
	s, _, err := loadSession(t, stubSessionID, stubLoads)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := turn(t.Context(), s, "the first prompt"); err != nil {
		t.Fatalf("the prompt failed: %v", err)
	}
	got, err := turn(t.Context(), s, "the second prompt")
	if err != nil {
		t.Fatalf("the prompt failed: %v", err)
	}
	same(t, got, updateEvents("the second prompt"))
}

// TestCollectTakesWhatArrivedBeforeTheRequestReturned drives collect without
// an agent, because the event it is about is the one the client put in the
// channel just as the request answered: the collector is then free to see the
// stop before it sees the event, which a real agent's timing rarely gives.
func TestCollectTakesWhatArrivedBeforeTheRequestReturned(t *testing.T) {
	s := &Session{events: make(chan Event, eventRoom)}
	got, err := s.collect(func() error {
		s.events <- Event{Type: TypeText, Text: "the last word of the history"}
		return nil
	})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	same(t, got, []Event{{Type: TypeText, Text: "the last word of the history", Replay: true}})
}

func TestLoadNamesASessionTheAgentDoesNotKnow(t *testing.T) {
	const gone = "sess-gone"
	_, _, err := loadSession(t, gone, stubLoads)
	if err == nil {
		t.Fatal("Load opened a session the agent does not have")
	}
	for _, want := range []string{gone, "stub", stubUnknownSession} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error is %v, and it does not say %q", err, want)
		}
	}
}

func TestLoadRefusesAnAgentThatCannotLoadBeforeItAsks(t *testing.T) {
	_, record, err := loadSession(t, stubSessionID, stubNoLoad)
	if err == nil {
		t.Fatal("Load used an agent that says it cannot load a session")
	}
	if !strings.Contains(err.Error(), "stub") {
		t.Errorf("the error is %v, and it should name the agent", err)
	}
	if _, err := os.Stat(record); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the agent recorded a request, and it should be refused before one is sent: %v", err)
	}
}
