package handler

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// start opens a session on the stub agent and closes it when the test ends,
// and gives back the record of what the agent saw and the agent's stderr.
func start(t *testing.T, cwd string) (*Session, string, *bytes.Buffer) {
	t.Helper()
	kind, record := stubKind(t)
	var stderr bytes.Buffer
	s, err := Start(t.Context(), kind, Policy{}, cwd, &stderr)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, record, &stderr
}

func TestStartOpensTheSessionTheAgentIssued(t *testing.T) {
	s, _, _ := start(t, t.TempDir())
	if s.ID() != stubSessionID {
		t.Errorf("ID() = %q, and the agent issued %q", s.ID(), stubSessionID)
	}
}

func TestStartAsksForFilesAndNotForATerminal(t *testing.T) {
	_, record, _ := start(t, t.TempDir())
	r := readRecord(t, record)
	if !r.Fs.ReadTextFile || !r.Fs.WriteTextFile {
		t.Errorf("the agent was offered fs %+v, and it wants both", r.Fs)
	}
	if r.Terminal {
		t.Error("the agent was offered a terminal, and the client serves none")
	}
}

func TestStartOpensTheSessionInTheDirectory(t *testing.T) {
	dir := t.TempDir()
	_, record, _ := start(t, dir)
	if got := readRecord(t, record).Cwd; got != dir {
		t.Errorf("the session cwd is %q, and the directory is %q", got, dir)
	}
}

func TestCloseEndsTheAgent(t *testing.T) {
	s, _, stderr := start(t, t.TempDir())
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := s.cmd.Process.Signal(os.Interrupt); !errors.Is(err, os.ErrProcessDone) {
		t.Errorf("the agent answered a signal after Close with %v, and it should be done", err)
	}
	if !strings.Contains(stderr.String(), stubHello) {
		t.Errorf("the agent's stderr is %q, and it wrote %q", stderr.String(), stubHello)
	}
}

func TestStartNamesACommandThatIsNotThere(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-agent")
	_, err := Start(t.Context(), Kind{Name: "stub", Argv: []string{missing}}, Policy{}, t.TempDir(), io.Discard)
	if err == nil {
		t.Fatal("Start found an agent that is not there")
	}
	if !strings.Contains(err.Error(), "no-such-agent") {
		t.Errorf("the error is %v, and it does not name the command", err)
	}
}

func TestStartRefusesAKindWithNoCommand(t *testing.T) {
	_, err := Start(t.Context(), Kind{Name: "empty"}, Policy{}, t.TempDir(), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("Start of a kind with no command gave %v, and the error should name the kind", err)
	}
}
