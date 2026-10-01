package run

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/handler"
	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

// acpConfig registers the fake as the default agent and leaves the runner at
// its default timeout.
func acpConfig(t *testing.T, dataDir string, lines ...string) config.Config {
	t.Helper()
	testfix.UseAgent(t, dataDir, "fake", testfix.FakeAgent(t), testfix.Script(t, lines...))
	return config.Config{Runs: 2, TimeoutMinutes: 60, DoneHours: 24, DefaultAgent: "fake"}
}

// shortTimeout installs a test-sized run limit and restores the configured
// behavior at cleanup.
func shortTimeout(t *testing.T, limit time.Duration) {
	t.Helper()
	was := runTimeout
	t.Cleanup(func() { runTimeout = was })
	runTimeout = func(config.Config) time.Duration { return limit }
}

// recordStops observes termination without signalling the test runner's
// process group, since Start runs in this process rather than a detached
// supervisor.
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

// eventsOf reads logged events, skipping agent stderr lines that are not JSON
// events.
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

func TestSessionOptionsGiveOnlyTheProjectCacheToTheAgent(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "cache", "projects", "7")
	options := sessionOptions(cacheDir)
	if !slices.Equal(options.AdditionalDirectories, []string{cacheDir}) {
		t.Errorf("additional directories = %v, want only %q", options.AdditionalDirectories, cacheDir)
	}
	wantEnvironment := []string{ProjectCacheEnvironment + "=" + cacheDir}
	if !slices.Equal(options.Environment, wantEnvironment) {
		t.Errorf("environment = %v, want %v", options.Environment, wantEnvironment)
	}
}

// A normal turn must record exit code zero and its event stream.
func TestTheACPRunnerTakesATurnThatEndedAsASuccess(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	cfg := acpConfig(t, dataDir, "text "+acpSaid, "stop end_turn")

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

func runUsageInt(n int) *int { return &n }

func TestTheACPRunnerStoresTheAggregateUsageTheAgentReported(t *testing.T) {
	tests := []struct {
		name    string
		report  string
		present bool
		want    store.AggregateUsage
	}{
		{
			name:    "every category",
			report:  `{"inputTokens":11,"cachedWriteTokens":2,"cachedReadTokens":3,"outputTokens":5,"thoughtTokens":4,"totalTokens":21}`,
			present: true,
			want: store.AggregateUsage{
				InputTokens:       runUsageInt(11),
				CachedWriteTokens: runUsageInt(2),
				CachedReadTokens:  runUsageInt(3),
				OutputTokens:      runUsageInt(5),
				ThoughtTokens:     runUsageInt(4),
				TotalTokens:       runUsageInt(21),
			},
		},
		{
			name:    "cache write omitted",
			report:  `{"inputTokens":11,"cachedReadTokens":3,"outputTokens":5,"thoughtTokens":4,"totalTokens":19}`,
			present: true,
			want: store.AggregateUsage{
				InputTokens:      runUsageInt(11),
				CachedReadTokens: runUsageInt(3),
				OutputTokens:     runUsageInt(5),
				ThoughtTokens:    runUsageInt(4),
				TotalTokens:      runUsageInt(19),
			},
		},
		{
			name:    "cache read omitted",
			report:  `{"inputTokens":11,"cachedWriteTokens":2,"outputTokens":5,"thoughtTokens":4,"totalTokens":18}`,
			present: true,
			want: store.AggregateUsage{
				InputTokens:       runUsageInt(11),
				CachedWriteTokens: runUsageInt(2),
				OutputTokens:      runUsageInt(5),
				ThoughtTokens:     runUsageInt(4),
				TotalTokens:       runUsageInt(18),
			},
		},
		{
			name:    "thought omitted",
			report:  `{"inputTokens":11,"cachedWriteTokens":2,"cachedReadTokens":3,"outputTokens":5,"totalTokens":21}`,
			present: true,
			want: store.AggregateUsage{
				InputTokens:       runUsageInt(11),
				CachedWriteTokens: runUsageInt(2),
				CachedReadTokens:  runUsageInt(3),
				OutputTokens:      runUsageInt(5),
				TotalTokens:       runUsageInt(21),
			},
		},
		{
			name:    "reported zeroes",
			report:  `{"inputTokens":0,"cachedWriteTokens":0,"cachedReadTokens":0,"outputTokens":0,"thoughtTokens":0,"totalTokens":0}`,
			present: true,
			want: store.AggregateUsage{
				InputTokens:       runUsageInt(0),
				CachedWriteTokens: runUsageInt(0),
				CachedReadTokens:  runUsageInt(0),
				OutputTokens:      runUsageInt(0),
				ThoughtTokens:     runUsageInt(0),
				TotalTokens:       runUsageInt(0),
			},
		},
		{name: "no usage object"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dataDir, id := queuedTicket(t, "Add the thing")
			lines := []string{"stop end_turn"}
			if test.report != "" {
				lines = append([]string{"usage " + test.report}, lines...)
			}
			if err := Start(testfix.OpenStore(t, dataDir), id, acpConfig(t, dataDir, lines...)); err != nil {
				t.Fatal(err)
			}
			s := testfix.OpenStore(t, dataDir)
			run, err := s.Run(id)
			if err != nil {
				t.Fatal(err)
			}
			got, present, err := s.RunUsage(run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if present != test.present || !reflect.DeepEqual(got, test.want) {
				t.Errorf("usage = (%+v, %t), want (%+v, %t)", got, present, test.want, test.present)
			}
		})
	}
}

// The command gives the supervisor one settings snapshot. A later change to
// the database must not change which agent this run starts.
func TestTheACPRunnerUsesTheDefaultAgentFromItsSnapshot(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	cfg := acpConfig(t, dataDir, "stop end_turn")
	s := testfix.OpenStore(t, dataDir)
	if err := s.SetDefaultAgent("codex"); err != nil {
		t.Fatal(err)
	}

	if err := Start(s, id, cfg); err != nil {
		t.Fatal(err)
	}
	if got := testfix.ReadTicket(t, dataDir, id).Session; got != "fake-1" {
		t.Errorf("session = %q, want the fake agent from the snapshot", got)
	}
}

func TestTheACPRunnerFailsWhenItsConfiguredModelCannotBeSelected(t *testing.T) {
	dataDir, id := queuedTicket(t, "Select the model")
	cfg := acpConfig(t, dataDir, "stop end_turn")
	cfg.DefaultModel = "model-v1"
	s := testfix.OpenStore(t, dataDir)
	// The fake advertises no model selector. The snapshot must still be
	// honored even though the stored default_model remains unset.
	err := Start(s, id, cfg)
	if err == nil || !strings.Contains(err.Error(), "cannot set model \"model-v1\"") {
		t.Fatalf("Start error = %v", err)
	}
	if ticket := testfix.ReadTicket(t, dataDir, id); ticket.Status != store.Failed || ticket.Session != "" {
		t.Fatalf("failed setup ticket = %+v", ticket)
	}
	run, err := s.Run(id)
	if err != nil || run.EndedAt.IsZero() {
		t.Fatalf("run was not closed: %+v, %v", run, err)
	}
}

// Use the ACP session ID rather than parsing agent output.
func TestTheACPRunnerPutsTheSessionOfTheAgentOnTheTicket(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	cfg := acpConfig(t, dataDir, "stop end_turn")

	if err := Start(testfix.OpenStore(t, dataDir), id, cfg); err != nil {
		t.Fatal(err)
	}

	if got := testfix.ReadTicket(t, dataDir, id).Session; got != "fake-1" {
		t.Errorf("session = %q, want %q", got, "fake-1")
	}
}

// A restart must load the prior run's session with the agent that created it,
// even when the configured default has changed. The replay marker distinguishes
// Load from a new session, while the failed prompt proves the old ID survives.
func TestRestartLoadsTheExistingSessionWithItsOriginalAgent(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	loaded := filepath.Join(t.TempDir(), "loaded")
	cfg := acpConfig(t, dataDir,
		"no-such-action",
		"history:",
		"write "+loaded+" replayed",
	)
	s := testfix.OpenStore(t, dataDir)
	if err := Start(s, id, cfg); err == nil {
		t.Fatal("err = nil, want the first prompt to fail")
	}
	first, err := s.Run(id)
	if err != nil {
		t.Fatal(err)
	}
	session := testfix.ReadTicket(t, dataDir, id).Session
	if session == "" {
		t.Fatal("the first run did not record its new session")
	}

	worktree := WorktreePath(dataDir, id)
	sentinel := filepath.Join(worktree, "first-run-work")
	if err := os.WriteFile(sentinel, []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	replacementRan := filepath.Join(t.TempDir(), "replacement-ran")
	testfix.UseAgent(t, dataDir, "replacement", testfix.FakeAgent(t),
		testfix.Script(t, "write "+replacementRan+" replacement", "stop end_turn"))
	cfg.DefaultAgent = "replacement"
	cfg.DefaultModel = "unavailable-new-default"

	if err := Restart(s, id, cfg); err == nil {
		t.Fatal("err = nil, want the loaded session's prompt to fail")
	}
	if got := testfix.WaitFor(t, loaded); got != "replayed" {
		t.Errorf("the load marker = %q, want %q", got, "replayed")
	}
	if _, err := os.Stat(replacementRan); !os.IsNotExist(err) {
		t.Errorf("the replacement default ran: %v", err)
	}
	if got := testfix.ReadTicket(t, dataDir, id).Session; got != session {
		t.Errorf("session after the failed prompt = %q, want %q", got, session)
	}
	if got, err := os.ReadFile(sentinel); err != nil || string(got) != "kept" {
		t.Errorf("work from the first run = %q, %v; want it kept", got, err)
	}
	second, err := s.Run(id)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID {
		t.Error("the restart reused the first run record")
	}
	if second.Agent != "fake" {
		t.Errorf("restart agent = %q, want the prior run's agent %q", second.Agent, "fake")
	}
}

func TestRestartPreservesConfirmedModelAcrossFailedLoads(t *testing.T) {
	for _, requested := range []string{"", "model-v2"} {
		t.Run("requested="+requested, func(t *testing.T) {
			dataDir, id := queuedTicket(t, "Keep the original model")
			selected := filepath.Join(t.TempDir(), "model")
			cfg := acpConfig(t, dataDir, "models: model-v1 model-v2", "selected-model "+selected, "stop refusal")
			cfg.DefaultModel = requested
			s := testfix.OpenStore(t, dataDir)
			if err := Start(s, id, cfg); err == nil {
				t.Fatal("expected refusal")
			}
			first, err := s.Run(id)
			want := requested
			if want == "" {
				want = "model-v1"
			}
			if err != nil || !first.ModelID.Valid || first.Model != want {
				t.Fatalf("first model = %+v, %v", first, err)
			}
			cfg.DefaultModel = "new-default"
			// The prior model must survive multiple failed setup attempts.
			testfix.UseAgent(t, dataDir, "fake", testfix.FakeAgent(t),
				testfix.Script(t, "models: model-v1 model-v2", "stop end_turn", "history:", "no-such-action"))
			for range 2 {
				if err := Restart(s, id, cfg); err == nil {
					t.Fatal("expected load failure")
				}
				latest, err := s.Run(id)
				if err != nil || latest.ModelID != first.ModelID {
					t.Fatalf("failed load lost model: %+v, %v", latest, err)
				}
			}
			// A now-unavailable prior model must fail before prompting.
			if err := os.Remove(selected); err != nil {
				t.Fatal(err)
			}
			testfix.UseAgent(t, dataDir, "fake", testfix.FakeAgent(t),
				testfix.Script(t, "models: new-default", "selected-model "+selected, "stop end_turn"))
			if err := Restart(s, id, cfg); err == nil || !strings.Contains(err.Error(), "unavailable") {
				t.Fatalf("unavailable model error = %v", err)
			}
			if _, err := os.Stat(selected); !os.IsNotExist(err) {
				t.Fatalf("prompt ran with unavailable model: %v", err)
			}
			// The fake's own default also changes; restart must explicitly
			// select the original model before its prompt runs.
			testfix.UseAgent(t, dataDir, "fake", testfix.FakeAgent(t),
				testfix.Script(t, "models: new-default model-v1 model-v2", "selected-model "+selected, "stop end_turn"))
			if err := Restart(s, id, cfg); err != nil {
				t.Fatal(err)
			}
			if got, err := os.ReadFile(selected); err != nil || string(got) != want {
				t.Fatalf("prompt model = %q, %v; want %q", got, err, want)
			}
			latest, err := s.Run(id)
			if err != nil || latest.ModelID != first.ModelID {
				t.Fatalf("restart model = %+v, %v", latest, err)
			}
		})
	}
}

func TestRestartKeepsTheExistingSessionWhenLoadFails(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	cfg := acpConfig(t, dataDir, "stop end_turn")
	s := testfix.OpenStore(t, dataDir)
	if err := Start(s, id, cfg); err != nil {
		t.Fatal(err)
	}
	if got := testfix.ReadTicket(t, dataDir, id).Session; got == "" {
		t.Fatal("the first run did not record its new session")
	}
	const session = "existing-session"
	testfix.SetSession(t, s.DataDir(), id, session)
	testfix.UseAgent(t, dataDir, "fake", testfix.FakeAgent(t),
		testfix.Script(t, "stop end_turn", "history:", "no-such-action"))

	if err := Restart(s, id, cfg); err == nil {
		t.Fatal("err = nil, want loading the session to fail")
	}
	if got := testfix.ReadTicket(t, dataDir, id).Session; got != session {
		t.Errorf("session after the failed load = %q, want %q", got, session)
	}
}

// The mid-turn marker proves the session ID was saved before completion,
// allowing interrupted runs to resume.
func TestTheACPRunnerRecordsTheSessionBeforeTheTurnEnds(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	marker := filepath.Join(t.TempDir(), "started")
	cfg := acpConfig(t, dataDir, "write "+marker+" go", "wait 1m", "stop end_turn")
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

// The runner starts the registry default, including an agent that is not part
// of the built-in catalog.
func TestTheACPRunnerStartsTheDefaultRegistryAgent(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	cfg := acpConfig(t, dataDir, "stop end_turn")

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
	if run.Agent != "fake" {
		t.Errorf("agent = %q, want %q", run.Agent, "fake")
	}
}

// Non-end_turn reasons must appear in both log and error, with noExitCode.
func TestTheACPRunnerFailsARunTheAgentStoppedForAnotherReason(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	cfg := acpConfig(t, dataDir, "text "+acpSaid, "stop refusal")

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

// Timeout must cancel the waiting ACP turn and fail the run.
func TestTheACPRunnerStopsATurnThatRanPastTheTimeout(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	cfg := acpConfig(t, dataDir, "text "+acpSaid, "wait 1m", "stop end_turn")
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

	if err := Start(testfix.OpenStore(t, dataDir), id, acpConfig(t, dataDir, "stop end_turn")); err != nil {
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
	cfg := acpConfig(t, dataDir, "write "+marker+" working", "wait 1m", "stop end_turn")

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

func TestTheACPRunnerRunsWithNoLimitWhenTheTimeoutIsZero(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	cfg := acpConfig(t, dataDir, "stop end_turn")
	cfg.TimeoutMinutes = 0

	if err := Start(testfix.OpenStore(t, dataDir), id, cfg); err != nil {
		t.Fatal(err)
	}
}
