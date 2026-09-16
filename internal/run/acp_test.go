package run

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/handler"
	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

// acpConfig replaces the agent with the fake and leaves the runner at its default.
func acpConfig(t *testing.T, lines ...string) config.Config {
	t.Helper()
	return loadConfig(t, fmt.Sprintf("[agents.claude]\nargv = [%q, %q]\n",
		testfix.FakeAgentPath, testfix.Script(t, lines...)))
}

// loadConfig writes the text as the config file of the person and gives it
// back read, for a test whose config acpConfig does not write.
func loadConfig(t *testing.T, text string) config.Config {
	t.Helper()
	dir := testfix.XDGConfigDir(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// shortTimeout puts a limit on a run that a test can wait for, and gives the
// config file's limit back at the end of the test.
func shortTimeout(t *testing.T, limit time.Duration) {
	t.Helper()
	was := runTimeout
	t.Cleanup(func() { runTimeout = was })
	runTimeout = func(config.Config) time.Duration { return limit }
}

// recordStops replaces the process-group stop with one a test can observe.
// Start runs in the process of the test, which is not a detached supervisor
// and must not signal the process group of the test runner.
func recordStops(t *testing.T) <-chan int {
	t.Helper()
	stopped := make(chan int, 1)
	was := stopRun
	t.Cleanup(func() { stopRun = was })
	stopRun = func(pid int, _ time.Duration) error {
		stopped <- pid
		return nil
	}
	return stopped
}

// eventsOf is the events in the log of a run. The agent's own stderr shares
// the log and is not an event, so a line that does not read as one is skipped.
func eventsOf(t *testing.T, dataDir string, id int64) []handler.Event {
	t.Helper()
	var got []handler.Event
	for e, err := range handler.ReadEvents(strings.NewReader(logOf(t, dataDir, id))) {
		if err == nil {
			got = append(got, e)
		}
	}
	return got
}

// statusOf is the status of a ticket.
func statusOf(t *testing.T, dataDir string, id int64) store.TicketStatus {
	t.Helper()
	return testfix.ReadTicket(t, dataDir, id).Status
}

const acpSaid = "the work is done"

// A turn that the agent ran to its end is a run that succeeded: exit code 0,
// no error, and every event of the turn in the log, one to the line.
func TestTheACPRunnerTakesATurnThatEndedAsASuccess(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	cfg := acpConfig(t, "text "+acpSaid, "stop end_turn")

	if err := Start(testfix.OpenStore(t, dataDir), id, cfg); err != nil {
		t.Fatal(err)
	}

	want := []handler.Event{
		{Type: handler.TypeText, Text: acpSaid},
		{Type: handler.TypeResult, Status: handler.StopEndTurn},
	}
	if got := eventsOf(t, dataDir, id); !slices.Equal(got, want) {
		t.Errorf("the log holds\n %+v\nwant\n %+v", got, want)
	}
	run, err := testfix.OpenStore(t, dataDir).Run(id)
	if err != nil {
		t.Fatal(err)
	}
	if !run.ExitCode.Valid || run.ExitCode.V != 0 {
		t.Errorf("exit code = %+v, want 0", run.ExitCode)
	}
}

// The agent gave the session its own id, and the supervisor took it from the
// session rather than from anything the agent printed.
func TestTheACPRunnerPutsTheSessionOfTheAgentOnTheTicket(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	cfg := acpConfig(t, "stop end_turn")

	if err := Start(testfix.OpenStore(t, dataDir), id, cfg); err != nil {
		t.Fatal(err)
	}

	if got := testfix.ReadTicket(t, dataDir, id).Session; got != "fake-1" {
		t.Errorf("session = %q, want %q", got, "fake-1")
	}
}

// The id is on the ticket before the turn ends, so a run that the timeout or a
// restart stops part way has left the id a person opens it with. The agent
// writes the marker in the middle of the turn and then waits, so a session
// read once the marker is there is one the supervisor recorded while the turn
// was still going.
func TestTheACPRunnerRecordsTheSessionBeforeTheTurnEnds(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	marker := filepath.Join(t.TempDir(), "started")
	cfg := acpConfig(t, "write "+marker+" go", "wait 1m", "stop end_turn")
	shortTimeout(t, 2*time.Second)

	done := make(chan error, 1)
	go func() { done <- Start(testfix.OpenStore(t, dataDir), id, cfg) }()

	testfix.WaitFor(t, marker)
	if got := testfix.ReadTicket(t, dataDir, id).Session; got != "fake-1" {
		t.Errorf("session part way through the turn = %q, want %q", got, "fake-1")
	}
	if err := <-done; err == nil {
		t.Error("err = nil, want the run the timeout stopped")
	}
}

// The runner starts the agent the config names and not one name of its own.
// The person here put the fake agent under a name that handler's table does
// not hold, and the session on the ticket is the one that fake gives, so the
// process that ran is the one the key named.
func TestTheACPRunnerStartsTheAgentOfTheConfig(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	cfg := loadConfig(t, fmt.Sprintf("agent = \"mine\"\n\n[agents.mine]\nargv = [%q, %q]\n",
		testfix.FakeAgentPath, testfix.Script(t, "stop end_turn")))

	if err := Start(testfix.OpenStore(t, dataDir), id, cfg); err != nil {
		t.Fatal(err)
	}

	if got := testfix.ReadTicket(t, dataDir, id).Session; got != "fake-1" {
		t.Errorf("session = %q, want %q", got, "fake-1")
	}
	run, err := testfix.OpenStore(t, dataDir).Run(id)
	if err != nil {
		t.Fatal(err)
	}
	if run.Agent != "mine" {
		t.Errorf("agent = %q, want %q", run.Agent, "mine")
	}
}

// An agent that the config names and delegator does not know stops the run at
// its first use, and the error names the agents there are: the person has to
// write one of those names, or a section that adds another.
func TestTheACPRunnerRefusesAnAgentItDoesNotKnow(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	cfg := loadConfig(t, "agent = \"mine\"\n")

	err := Start(testfix.OpenStore(t, dataDir), id, cfg)
	if err == nil {
		t.Fatal("err = nil, want the agent that delegator does not know")
	}
	want := []string{"mine"}
	for _, kind := range handler.Kinds() {
		want = append(want, kind.Name)
	}
	for _, name := range want {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("the error does not name %q: %v", name, err)
		}
	}
	if got := statusOf(t, dataDir, id); got != store.Failed {
		t.Errorf("status = %v, want %v", got, store.Failed)
	}
}

// A reason that is not end_turn is a turn that something stopped, and the run
// failed. The reason is in the log and in the error, and the exit code is the
// one of a run with no process of its own to give one.
func TestTheACPRunnerFailsARunTheAgentStoppedForAnotherReason(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	cfg := acpConfig(t, "text "+acpSaid, "stop refusal")

	err := Start(testfix.OpenStore(t, dataDir), id, cfg)
	if err == nil {
		t.Fatal("err = nil, want the reason the agent stopped")
	}
	if !strings.Contains(err.Error(), "refusal") {
		t.Errorf("the error is %v, and it does not name the reason", err)
	}

	events := eventsOf(t, dataDir, id)
	if len(events) == 0 || events[len(events)-1].Status != "refusal" {
		t.Errorf("the log ends with %+v, want a result of refusal", events)
	}
	if got := statusOf(t, dataDir, id); got != store.Failed {
		t.Errorf("status = %v, want %v", got, store.Failed)
	}
	run, err := testfix.OpenStore(t, dataDir).Run(id)
	if err != nil {
		t.Fatal(err)
	}
	if !run.ExitCode.Valid || run.ExitCode.V != noExitCode {
		t.Errorf("exit code = %+v, want %d", run.ExitCode, noExitCode)
	}
	if run.EndedAt.IsZero() {
		t.Error("the run that failed has no end time")
	}
}

// The timeout goes down the context, so a turn that runs past it is cancelled
// and the run fails. The agent is left waiting and the reason is cancelled.
func TestTheACPRunnerStopsATurnThatRanPastTheTimeout(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	cfg := acpConfig(t, "text "+acpSaid, "wait 1m", "stop end_turn")
	shortTimeout(t, 200*time.Millisecond)
	stopped := recordStops(t)

	if err := Start(testfix.OpenStore(t, dataDir), id, cfg); err == nil {
		t.Fatal("err = nil, want the run the timeout stopped")
	}

	want := []handler.Event{
		{Type: handler.TypeText, Text: acpSaid},
		{Type: handler.TypeResult, Status: "cancelled"},
	}
	if got := eventsOf(t, dataDir, id); !slices.Equal(got, want) {
		t.Errorf("the log holds\n %+v\nwant\n %+v", got, want)
	}
	if got := statusOf(t, dataDir, id); got != store.Failed {
		t.Errorf("status = %v, want %v", got, store.Failed)
	}
	select {
	case pid := <-stopped:
		if pid != os.Getpid() {
			t.Errorf("the timeout stopped process %d, want supervisor %d", pid, os.Getpid())
		}
	default:
		t.Error("the timeout did not stop the supervisor's process group")
	}
	run, err := testfix.OpenStore(t, dataDir).Run(id)
	if err != nil {
		t.Fatal(err)
	}
	if run.EndedAt.IsZero() {
		t.Error("the timed out run has no end time")
	}
}

// A run that reaches its ordinary end disarms the timer. Waiting past the
// short limit proves that no late callback can stop a later use of the same
// supervisor process.
func TestTheACPRunnerThatEndsBeforeTheTimeoutIsNotStopped(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	shortTimeout(t, 100*time.Millisecond)
	stopped := recordStops(t)

	if err := Start(testfix.OpenStore(t, dataDir), id, acpConfig(t, "stop end_turn")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	select {
	case pid := <-stopped:
		t.Errorf("the timeout stopped process %d after the run ended", pid)
	default:
	}
}

// dg finish can win the race with the timeout. The timeout still stops an
// agent that continues to work, but it leaves ready as the report of the run.
func TestTheACPRunnerTimeoutKeepsATicketThatFinishedFirstReady(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	s := testfix.OpenStore(t, dataDir)
	marker := filepath.Join(t.TempDir(), "working")
	shortTimeout(t, 200*time.Millisecond)
	stopped := recordStops(t)
	cfg := acpConfig(t, "write "+marker+" working", "wait 1m", "stop end_turn")

	done := make(chan error, 1)
	go func() {
		done <- Start(s, id, cfg)
	}()
	testfix.WaitFor(t, marker)
	commit := testfix.GitOut(t, WorktreePath(dataDir, id), "rev-parse", "HEAD")
	if err := s.FinishTicket(id, commit); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Error("err = nil, want the timeout that stopped the run")
	}
	if got := statusOf(t, dataDir, id); got != store.Ready {
		t.Errorf("status = %v, want %v", got, store.Ready)
	}
	select {
	case <-stopped:
	default:
		t.Error("the timeout did not stop the run after dg finish")
	}
}

// A timeout of 0 in the config is no limit, and the run takes as long as the
// agent does.
func TestTheACPRunnerRunsWithNoLimitWhenTheTimeoutIsZero(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	cfg := acpConfig(t, "stop end_turn")
	cfg.TimeoutMinutes = 0

	if err := Start(testfix.OpenStore(t, dataDir), id, cfg); err != nil {
		t.Fatal(err)
	}
}
