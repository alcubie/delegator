package run

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

type failedTicket struct {
	s        *store.Store
	dataDir  string
	id       int64
	session  string
	sent     string
	replayed string
}

// restartAfter fails a first run, then restarts the ticket with the clock
// idle after the end of that run. The agent of the restart records the prompt
// it receives, and writes a marker when it replays the history of a loaded
// session.
func restartAfter(t *testing.T, idle time.Duration) failedTicket {
	t.Helper()
	dataDir, id := queuedTicket(t, "a title")
	cfg := acpConfig(t, dataDir, "stop refusal")
	s := testfix.OpenStore(t, dataDir)
	if err := Start(s, id, cfg); err == nil {
		t.Fatal("err = nil, want the refused turn to fail the first run")
	}
	first, err := s.Run(id)
	if err != nil {
		t.Fatal(err)
	}
	const session = "fake-7"
	testfix.SetSession(t, dataDir, id, session)
	sent := filepath.Join(t.TempDir(), "prompt")
	replayed := filepath.Join(t.TempDir(), "replayed")
	testfix.UseAgent(t, dataDir, "fake", testfix.FakeAgent(t),
		testfix.Script(t, "prompt "+sent, "stop end_turn", "history:", "write "+replayed+" replayed"))

	clock := now
	now = func() time.Time { return first.EndedAt.Add(idle) }
	t.Cleanup(func() { now = clock })

	if err := Restart(s, id, cfg, ""); err != nil {
		t.Fatal(err)
	}
	return failedTicket{s: s, dataDir: dataDir, id: id, session: session, sent: sent, replayed: replayed}
}

func (f failedTicket) lastLog(t *testing.T) string {
	t.Helper()
	logs, err := filepath.Glob(filepath.Join(LogDirPath(f.dataDir, f.id), "*.log"))
	if err != nil || len(logs) == 0 {
		t.Fatalf("logs = %v, %v", logs, err)
	}
	got, err := os.ReadFile(logs[len(logs)-1])
	if err != nil {
		t.Fatal(err)
	}
	return string(got)
}

func TestRestartTwoHoursLaterStartsANewSession(t *testing.T) {
	f := restartAfter(t, 2*time.Hour)

	if _, err := os.Stat(f.replayed); err == nil {
		t.Error("the restart loaded the session of the ticket")
	}
	if got := testfix.ReadTicket(t, f.dataDir, f.id).Session; got == "" || got == f.session {
		t.Errorf("the session of the ticket is %q, want a session other than %q", got, f.session)
	}
	if got := f.lastLog(t); !strings.Contains(got, "delegator: last run ended 2h0m0s ago, new session") {
		t.Errorf("the log does not say why the session is new:\n%s", got)
	}
}

func TestRestartTenMinutesLaterLoadsTheSession(t *testing.T) {
	f := restartAfter(t, 10*time.Minute)

	if _, err := os.Stat(f.replayed); err != nil {
		t.Errorf("the restart did not load the session: %v", err)
	}
	if got := testfix.ReadTicket(t, f.dataDir, f.id).Session; got != f.session {
		t.Errorf("the session of the ticket is %q, want %q", got, f.session)
	}
	if got := f.lastLog(t); !strings.Contains(got, "delegator: last run ended 10m0s ago, loaded session") {
		t.Errorf("the log does not say which session the run used:\n%s", got)
	}
}

func TestPromptOfARestartHoldsTheErrorOfTheFailedRun(t *testing.T) {
	f := restartAfter(t, 2*time.Hour)
	run, err := f.s.Runs(f.id)
	if err != nil || len(run) < 2 {
		t.Fatalf("runs = %v, %v", run, err)
	}
	failure := run[1].Error
	if failure == "" {
		t.Fatal("the first run recorded no error")
	}

	got := testfix.WaitFor(t, f.sent)

	for _, want := range []string{failure, "git log main..HEAD", "git status", "dg ticket show"} {
		if !strings.Contains(got, want) {
			t.Errorf("the prompt does not hold %q:\n%s", want, got)
		}
	}
}
