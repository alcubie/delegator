package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

// collect returns all events and errors in stream order.
func collect(r io.Reader) ([]Event, []error) {
	var events []Event
	var errs []error
	for e, err := range ReadEvents(r) {
		if err != nil {
			errs = append(errs, err)
			continue
		}
		events = append(events, e)
	}
	return events, errs
}

// write encodes the events onto one buffer, the way a run writes its stream.
func write(t *testing.T, events ...Event) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	for _, e := range events {
		if err := WriteEvent(&buf, e); err != nil {
			t.Fatalf("WriteEvent(%v): %v", e, err)
		}
	}
	return &buf
}

// errReader returns its data, then a read error, simulating a failed pipe.
type errReader struct {
	data string
	err  error
	done bool
}

func (r *errReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, r.err
	}
	r.done = true
	return copy(p, r.data), nil
}

// errWriter fails on every write.
type errWriter struct{ err error }

func (w *errWriter) Write([]byte) (int, error) { return 0, w.err }

func TestRoundTripOfEveryType(t *testing.T) {
	t.Parallel()
	events := []Event{
		{Type: TypeText, Text: "Looking at the file."},
		{Type: TypeThought, Text: "The test names the field, so the field is wrong."},
		{Type: TypeTool, Tool: "Edit", Summary: "internal/cli/run.go", Kind: "edit", Status: "pending"},
		{Type: TypeToolUpdate, Tool: "Edit", Summary: "internal/cli/run.go", Kind: "edit", Status: "completed"},
		{Type: TypePermission, Tool: "Edit internal/cli/run.go", Kind: "edit", Status: StatusAllowed},
		{Type: TypeReplay, Text: "what the agent said before the session was loaded", Replay: true},
		{Type: TypeResult, Text: "REPLY a1b2c3d4: done", Session: "d26a4837", Code: 0},
		{Type: TypeError, Err: "the agent timed out", Code: 124},
	}
	got, errs := collect(write(t, events...))
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	if len(got) != len(events) {
		t.Fatalf("read %d events, want %d: %v", len(got), len(events), got)
	}
	for i, want := range events {
		if got[i] != want {
			t.Errorf("event %d is %+v, want %+v", i, got[i], want)
		}
	}
}

func TestGoldenBytes(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		event Event
		want  string
	}{
		{
			Event{Type: TypeTool, Tool: "Edit", Summary: "internal/cli/run.go", Kind: "edit", Status: "pending"},
			`{"type":"tool","tool":"Edit","summary":"internal/cli/run.go","kind":"edit","status":"pending"}`,
		},
		{
			Event{Type: TypePermission, Tool: "Edit internal/cli/run.go", Kind: "edit", Status: StatusRejected},
			`{"type":"permission","tool":"Edit internal/cli/run.go","kind":"edit","status":"rejected"}`,
		},
		{
			Event{Type: TypeText, Text: "what the agent said before", Replay: true},
			`{"type":"text","text":"what the agent said before","replay":true}`,
		},
	} {
		got := write(t, c.event).String()
		if want := c.want + "\n"; got != want {
			t.Errorf("wrote %q, want %q", got, want)
		}
	}
}

func TestHTMLCharactersStayLiteral(t *testing.T) {
	t.Parallel()
	const want = `{"type":"text","text":"if a < b && c"}` + "\n"
	got := write(t, Event{Type: TypeText, Text: "if a < b && c"}).String()
	if got != want {
		t.Errorf("wrote %q, want %q: a reader of the stream wants the characters, not their escapes", got, want)
	}
}

func TestUnknownFieldIsIgnored(t *testing.T) {
	t.Parallel()
	const line = `{"type":"text","text":"hello","parent_tool_use_id":"toolu_01","cost":0.12}` + "\n"
	got, errs := collect(strings.NewReader(line))
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	want := []Event{{Type: TypeText, Text: "hello"}}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("read %+v, want %+v", got, want)
	}
}

func TestMalformedLineReportsAndReadingContinues(t *testing.T) {
	t.Parallel()
	const lines = `{"type":"text","text":"first"}` + "\n" +
		"{not json\n" +
		`{"type":"text","text":"second"}` + "\n"
	got, errs := collect(strings.NewReader(lines))
	if len(errs) != 1 {
		t.Fatalf("reported %v, want one error", errs)
	}
	var syntax *json.SyntaxError
	if !errors.As(errs[0], &syntax) {
		t.Errorf("error is %v, want it to wrap a json syntax error", errs[0])
	}
	if !strings.Contains(errs[0].Error(), "line 2") {
		t.Errorf("error is %v, want it to name line 2", errs[0])
	}
	want := []Event{{Type: TypeText, Text: "first"}, {Type: TypeText, Text: "second"}}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("read %+v, want %+v", got, want)
	}
}

func TestBlankLineIsSkipped(t *testing.T) {
	t.Parallel()
	const lines = "\n" + `{"type":"text","text":"hello"}` + "\n" + "  \n\n"
	got, errs := collect(strings.NewReader(lines))
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	if len(got) != 1 || got[0].Text != "hello" {
		t.Errorf("read %+v, want the one event", got)
	}
}

func TestLastLineWithoutANewlineIsRead(t *testing.T) {
	t.Parallel()
	got, errs := collect(strings.NewReader(`{"type":"text","text":"hello"}`))
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	if len(got) != 1 || got[0].Text != "hello" {
		t.Errorf("read %+v, want the one event", got)
	}
}

func TestLineLongerThanTheReadBufferIsRead(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 128*1024)
	got, errs := collect(write(t, Event{Type: TypeText, Text: long}))
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	if len(got) != 1 || got[0].Text != long {
		t.Errorf("read %d events, want the one long event", len(got))
	}
}

func TestReadEventsReportsAReadError(t *testing.T) {
	t.Parallel()
	broken := errors.New("the pipe is closed")
	r := &errReader{data: `{"type":"text","text":"hello"}` + "\n", err: broken}
	got, errs := collect(r)
	if len(got) != 1 {
		t.Errorf("read %+v, want the event before the failure", got)
	}
	if len(errs) != 1 || !errors.Is(errs[0], broken) {
		t.Fatalf("reported %v, want the read error", errs)
	}
}

func TestReadEventsStopsWhenTheCallerStops(t *testing.T) {
	t.Parallel()
	const lines = `{"type":"text","text":"first"}` + "\n" +
		`{"type":"text","text":"second"}` + "\n"
	var got []Event
	for e, err := range ReadEvents(strings.NewReader(lines)) {
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		got = append(got, e)
		break
	}
	if len(got) != 1 || got[0].Text != "first" {
		t.Errorf("read %+v, want only the first event", got)
	}
}

func TestReadEventsStopsWhenTheCallerStopsAtAnError(t *testing.T) {
	t.Parallel()
	const lines = "{not json\n" + `{"type":"text","text":"second"}` + "\n"
	seen := 0
	for range ReadEvents(strings.NewReader(lines)) {
		seen++
		break
	}
	if seen != 1 {
		t.Errorf("the loop ran %d times, want one", seen)
	}
}

func TestWriteEventReportsAWriteError(t *testing.T) {
	t.Parallel()
	broken := errors.New("the pipe is closed")
	err := WriteEvent(&errWriter{err: broken}, Event{Type: TypeText, Text: "hello"})
	if !errors.Is(err, broken) {
		t.Errorf("WriteEvent gave %v, want the write error", err)
	}
}
