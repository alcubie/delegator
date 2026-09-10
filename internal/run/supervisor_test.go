package run

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/adapters"
	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

func TestMain(m *testing.M) {
	os.Exit(testfix.RunTests(m, false))
}

// queuedTicket makes a data directory that holds one project and one ticket in
// the queue, and returns the directory and the id of the ticket.
//
// It leaves the repository with HEAD on a branch that is not main, which is
// where a person usually is. A run that starts from HEAD rather than from the
// default branch therefore fails a test instead of passing on a fixture that
// has one branch.
func queuedTicket(t *testing.T, title string) (string, int64) {
	t.Helper()
	repo := repoOnMain(t)
	dataDir := t.TempDir()

	s := testfix.OpenStore(t, dataDir)

	projectID, err := s.ProjectID(repo, "main")
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.AddTicket(projectID, title)
	if err != nil {
		t.Fatal(err)
	}

	testfix.GitIn(t, repo, "checkout", "-q", "-b", "other")
	testfix.CommitIn(t, repo, "second")
	return dataDir, id
}

// recordingAgent is an Adapter that keeps the output it was given at each
// call of SessionID, so a test sees when the supervisor asked and with what.
type recordingAgent struct {
	adapters.Adapter
	asked []string
}

func (r *recordingAgent) SessionID(out []byte) (string, error) {
	r.asked = append(r.asked, string(out))
	return r.Adapter.SessionID(out)
}

// The branch on the ticket is the mark of the claim: only Claim writes it,
// and it names the branch the worktree is on. The status when Start returns
// is failed, because this agent gives no report, and the tests below are the
// ones that examine it.
func TestStartMakesTheWorktreeAndClaimsTheTicket(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")

	if err := Start(testfix.OpenStore(t, dataDir), id, fakeAgent(t, "exit 0")); err != nil {
		t.Fatal(err)
	}

	ticket := testfix.ReadTicket(t, dataDir, id)
	want := branch(id, "Add the thing")
	if ticket.Branch != want {
		t.Errorf("branch = %q, want %q", ticket.Branch, want)
	}

	worktree := filepath.Join(dataDir, "worktrees", "1")
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("the worktree is not there: %v", err)
	}
	if got := testfix.GitOut(t, worktree, "rev-parse", "--abbrev-ref", "HEAD"); got != want {
		t.Errorf("the worktree is on %q, want %q", got, want)
	}
	if got, base := testfix.GitOut(t, worktree, "rev-parse", "HEAD"), testfix.GitOut(t, ticket.Project.Path, "rev-parse", "main"); got != base {
		t.Errorf("the worktree starts at %s, want main at %s", got, base)
	}
}

// A supervisor starts the next ticket when its own run ends, so two can reach
// one ticket at the same time. The claim is the first thing each one does, so
// the database is what refuses the second.
func TestStartGivesOneTicketToOneRun(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")

	agent := fakeAgent(t, "exit 0")
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		s := testfix.OpenStore(t, dataDir)
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- Start(s, id, agent)
		}()
	}
	wg.Wait()
	close(errs)

	won := 0
	for err := range errs {
		switch {
		case err == nil:
			won++
		case !errors.Is(err, store.ErrInvalidTicketStateChange):
			t.Errorf("err = %v, want ErrInvalidTicketStateChange", err)
		}
	}
	if won != 1 {
		t.Errorf("%d supervisors took the ticket, want 1", won)
	}
	// The one that won ran the agent to its end, and the agent gave no
	// report, so the ticket it holds is failed and not running.
	if got := testfix.ReadTicket(t, dataDir, id); got.Status != store.Failed {
		t.Errorf("status = %q, want %q", got.Status, store.Failed)
	}
}

// dg run with no id is what a trigger starts. The supervisor takes the first
// ticket of the queue for itself and works it, so no trigger has to name one.
func TestStartNextTakesTheFirstTicketOfTheQueueAndRunsIt(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	second := testfix.SecondTicket(t, dataDir)

	if err := StartNext(testfix.OpenStore(t, dataDir), fakeAgent(t, "write made-by-the-agent done", "exit 0")); err != nil {
		t.Fatal(err)
	}

	ticket := testfix.ReadTicket(t, dataDir, id)
	// The agent of this test does not call dg finish, so the run gave no
	// report and the supervisor failed the ticket as it stopped.
	if ticket.Status != store.Failed {
		t.Errorf("status = %q, want %q", ticket.Status, store.Failed)
	}
	if ticket.Branch != branch(id, "Add the thing") {
		t.Errorf("branch = %q, want %q", ticket.Branch, branch(id, "Add the thing"))
	}
	made := filepath.Join(WorktreePath(dataDir, id), "made-by-the-agent")
	if _, err := os.Stat(made); err != nil {
		t.Errorf("the agent did not run in the worktree of the ticket it claimed: %v", err)
	}
	if got := testfix.ReadTicket(t, dataDir, second).Status; got != store.Queued {
		t.Errorf("the second ticket is %q, want %q", got, store.Queued)
	}
}

// A trigger starts a supervisor for each free slot, and a supervisor that
// finds the queue full or empty by the time it reads it has nothing to do. It
// stops, and the trigger that started it reports no error.
func TestStartNextWithNothingToClaimStopsWithNoError(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	s := testfix.OpenStore(t, dataDir)
	if _, err := s.Claim(id, branch(id, "Add the thing")); err != nil {
		t.Fatal(err)
	}

	if err := StartNext(s, fakeAgent(t, "exit 0")); err != nil {
		t.Fatalf("err = %v, want nil from a supervisor with nothing to claim", err)
	}

	if _, err := os.Stat(WorktreePath(dataDir, id)); err == nil {
		t.Error("a supervisor with nothing to claim made a worktree")
	}
}

// A ticket in running is one that a different supervisor holds. The claim is
// the first thing Start does, so the refusal comes before any worktree, and
// the run the other supervisor is keeping is left as it was.
func TestStartRefusesATicketThatIsAlreadyRunning(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	s := testfix.OpenStore(t, dataDir)
	if _, err := s.Claim(id, branch(id, "Add the thing")); err != nil {
		t.Fatal(err)
	}

	err := Start(s, id, fakeAgent(t, "exit 0"))

	if !errors.Is(err, store.ErrInvalidTicketStateChange) {
		t.Fatalf("err = %v, want ErrInvalidTicketStateChange", err)
	}
	held, err := s.Run(id)
	if err != nil {
		t.Fatal(err)
	}
	if !held.EndedAt.IsZero() {
		t.Errorf("the refused start ended the run of the supervisor that holds the ticket at %v", held.EndedAt)
	}
	if _, err := os.Stat(WorktreePath(dataDir, id)); err == nil {
		t.Error("the refused start made a worktree")
	}
}

// The supervisor claims before it makes its worktree, because the claim is
// what gives one ticket to one supervisor. A ticket whose worktree git will
// not make is therefore already claimed, and the supervisor that holds it is
// about to stop, so it marks the ticket failed and ends its run. Left in
// running, the ticket would hold the queue with nothing working on it.
func TestStartThatCannotMakeTheWorktreeLeavesTheTicketFailed(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	if err := os.RemoveAll(testfix.ReadTicket(t, dataDir, id).Project.Path); err != nil {
		t.Fatal(err)
	}
	s := testfix.OpenStore(t, dataDir)

	if err := Start(s, id, fakeAgent(t, "exit 0")); err == nil {
		t.Fatal("err = nil, want the failure to make the worktree")
	}

	if got := testfix.ReadTicket(t, dataDir, id).Status; got != store.Failed {
		t.Errorf("status = %q, want %q", got, store.Failed)
	}
	failed, err := s.Run(id)
	if err != nil {
		t.Fatal(err)
	}
	if failed.EndedAt.IsZero() {
		t.Error("the run of a ticket that never started has no end time")
	}
}

// fakeAgent returns an Adapter that runs the given script. It is here and not
// in testfix because testfix cannot import adapters.
func fakeAgent(t *testing.T, lines ...string) adapters.Fake {
	t.Helper()
	return adapters.Fake{Binary: testfix.FakeAgentPath, Script: testfix.Script(t, lines...)}
}

// The script sleeps before it writes, so a Start that returns without waiting
// finds no file. The path is relative, so the file lands in the worktree only
// if the agent was started there.
func TestStartRunsTheAgentInTheWorktreeAndWaits(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	agent := fakeAgent(t, "run sleep 0.3", "write made-by-the-agent done", "exit 0")

	if err := Start(testfix.OpenStore(t, dataDir), id, agent); err != nil {
		t.Fatal(err)
	}

	made := filepath.Join(dataDir, "worktrees", "1", "made-by-the-agent")
	content, err := os.ReadFile(made)
	if err != nil {
		t.Fatalf("the agent did not finish before Start returned: %v", err)
	}
	if got := string(content); got != "done" {
		t.Errorf("content = %q, want %q", got, "done")
	}
}

// logOf returns the one log a run wrote below runs/<id>.
func logOf(t *testing.T, dataDir string, id int64) string {
	t.Helper()
	logs, err := filepath.Glob(filepath.Join(dataDir, "runs", strconv.FormatInt(id, 10), "*.log"))
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 {
		t.Fatalf("logs = %v, want one", logs)
	}
	data, err := os.ReadFile(logs[0])
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestStartRecordsTheSessionTheRunReported(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	agent := fakeAgent(t, "run printf 'session: s-1\\n'", "exit 0")

	if err := Start(testfix.OpenStore(t, dataDir), id, agent); err != nil {
		t.Fatal(err)
	}

	if got := testfix.ReadTicket(t, dataDir, id).Session; got != "s-1" {
		t.Errorf("session = %q, want %q", got, "s-1")
	}
}

// The run that fails is the one a person most wants to open, so its session
// is recorded before its failure is reported.
func TestStartRecordsTheSessionOfARunThatFailed(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	agent := fakeAgent(t, "run printf 'session: s-1\\n'", "exit 3")

	if err := Start(testfix.OpenStore(t, dataDir), id, agent); err == nil {
		t.Fatal("err = nil, want the exit status of the run")
	}

	if got := testfix.ReadTicket(t, dataDir, id).Session; got != "s-1" {
		t.Errorf("session = %q, want %q", got, "s-1")
	}
}

// The end time and the exit code are facts of the run, and the supervisor is
// the one program that has the exit code. An exit of 0 is recorded as much as
// an exit of 3.
func TestStartWritesTheEndTimeAndTheExitCodeWhenTheRunEnds(t *testing.T) {
	for _, exitCode := range []int{0, 3} {
		dataDir, id := queuedTicket(t, "Add the thing")
		agent := fakeAgent(t, "exit "+strconv.Itoa(exitCode))

		err := Start(testfix.OpenStore(t, dataDir), id, agent)
		if (err != nil) != (exitCode != 0) {
			t.Fatalf("exit %d: err = %v", exitCode, err)
		}

		run, err := testfix.OpenStore(t, dataDir).Run(id)
		if err != nil {
			t.Fatal(err)
		}
		if !run.ExitCode.Valid || run.ExitCode.V != exitCode {
			t.Errorf("exit code = %+v, want %d", run.ExitCode, exitCode)
		}
		if run.EndedAt.IsZero() || run.EndedAt.Before(run.StartedAt) {
			t.Errorf("ended at = %v, want a time not before the start at %v", run.EndedAt, run.StartedAt)
		}
	}
}

// The log holds what the agent wrote on both streams, so a person can read a
// run that produced no commit.
func TestStartWritesTheLogOfTheRun(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	agent := fakeAgent(t, "run printf 'to stdout\\n'", "run printf 'to stderr\\n' >&2", "exit 0")

	if err := Start(testfix.OpenStore(t, dataDir), id, agent); err != nil {
		t.Fatal(err)
	}

	log := logOf(t, dataDir, id)
	for _, want := range []string{"to stdout", "to stderr"} {
		if !strings.Contains(log, want) {
			t.Errorf("the log does not hold %q:\n%s", want, log)
		}
	}
}

// The agent learns the ticket through dg show and ends through dg finish,
// so the prompt must name both with the right id.
func TestPromptNamesBothCommands(t *testing.T) {
	got := prompt(42)
	for _, want := range []string{"dg show 42", "dg finish 42"} {
		if !strings.Contains(got, want) {
			t.Errorf("the prompt does not hold %q:\n%s", want, got)
		}
	}
}

// A run that reads the repository by cat carries every file it opened in the
// context for the rest of the run, so the prompt must hold the rules that say
// what to read instead.
func TestPromptSaysHowToRead(t *testing.T) {
	got := prompt(42)
	for _, want := range []string{"grep", "200 lines", "100 lines"} {
		if !strings.Contains(got, want) {
			t.Errorf("the prompt does not hold %q:\n%s", want, got)
		}
	}
}

// The supervisor reads the output line by line, and a reader of lines has two
// ways to lose part of the output: a line longer than its buffer, which a
// tool result in the stream of a real agent often is, and a last line that no
// newline ends. The log must hold both whole.
func TestStartWritesEachLineOfTheRunToTheLogInFull(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	longLine := strings.Repeat("x", 70_000)
	agent := fakeAgent(t,
		"run printf 'one\\n'",
		"run head -c 70000 /dev/zero | tr '\\0' x; printf '\\n'",
		"run printf 'last'",
		"exit 0",
	)

	if err := Start(testfix.OpenStore(t, dataDir), id, agent); err != nil {
		t.Fatal(err)
	}

	if got, want := logOf(t, dataDir, id), "one\n"+longLine+"\nlast"; got != want {
		t.Errorf("the log holds %d bytes, want %d:\n%.80s", len(got), len(want), got)
	}
}

// The agent reports its session and then sleeps, and the test reads the
// session from the ticket while the agent is alive. A supervisor that writes
// the session after the run gives none until Start returns.
func TestStartWritesTheSessionWhileTheAgentIsAlive(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	agent := fakeAgent(t, "run printf 'session: s-1\\n'", "run sleep 1", "exit 0")

	done := make(chan error, 1)
	go func() { done <- Start(testfix.OpenStore(t, dataDir), id, agent) }()

	s := testfix.OpenStore(t, dataDir)
	deadline := time.Now().Add(5 * time.Second)
	for {
		ticket, err := s.Ticket(id)
		if err != nil {
			t.Fatal(err)
		}
		if ticket.Session == "s-1" {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("Start returned (err = %v) and the ticket had no session while the agent was alive", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("session = %q after 5s, want %q", ticket.Session, "s-1")
		}
		time.Sleep(10 * time.Millisecond)
	}

	select {
	case err := <-done:
		t.Fatalf("Start returned (err = %v) before the ticket had a session", err)
	default:
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// The supervisor gives the adapter the output so far after each line, and
// stops asking once the adapter has answered. The first line holds no
// session and the second does, so there are two calls and not four.
func TestStartAsksForTheSessionAfterEachLineUntilItHasOne(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	agent := &recordingAgent{Adapter: fakeAgent(t,
		"run printf 'hello\\n'",
		"run printf 'session: s-1\\n'",
		"run printf 'more\\nmore\\n'",
		"exit 0",
	)}

	if err := Start(testfix.OpenStore(t, dataDir), id, agent); err != nil {
		t.Fatal(err)
	}

	want := []string{"hello\n", "hello\nsession: s-1\n"}
	if !slices.Equal(agent.asked, want) {
		t.Errorf("SessionID was asked with %q, want %q", agent.asked, want)
	}
	if got := testfix.ReadTicket(t, dataDir, id).Session; got != "s-1" {
		t.Errorf("session = %q, want %q", got, "s-1")
	}
}

// A run that ends before it reports an id keeps no session, and that is not
// a fault of the run: its exit status already says what happened.
func TestStartKeepsNoSessionWhenTheRunReportsNone(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	agent := fakeAgent(t, "run printf 'it stopped early\\n'", "exit 0")

	if err := Start(testfix.OpenStore(t, dataDir), id, agent); err != nil {
		t.Fatal(err)
	}

	if got := testfix.ReadTicket(t, dataDir, id).Session; got != "" {
		t.Errorf("session = %q, want none", got)
	}
}

// The id on the last line still reaches the ticket, and the last line of a
// run has no newline after it when the agent stops without one.
func TestStartRecordsTheSessionOnTheLastLineWithNoNewline(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")
	agent := fakeAgent(t, "run printf 'the work\\nsession: s-1'", "exit 0")

	if err := Start(testfix.OpenStore(t, dataDir), id, agent); err != nil {
		t.Fatal(err)
	}

	if got := testfix.ReadTicket(t, dataDir, id).Session; got != "s-1" {
		t.Errorf("session = %q, want %q", got, "s-1")
	}
}

// A run that gave no report did not succeed. The agent here exits without
// dg finish, which is the only thing that makes a ticket ready, so the
// supervisor fails the ticket itself before it stops. Without this a ticket
// whose supervisor is there and whose run ended would stay in running, and
// only the reconcile of a later command would move it.
func TestStartFailsATicketThatTheAgentDidNotFinish(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")

	if err := Start(testfix.OpenStore(t, dataDir), id, fakeAgent(t, "exit 0")); err != nil {
		t.Fatal(err)
	}

	if got := testfix.ReadTicket(t, dataDir, id); got.Status != store.Failed {
		t.Errorf("status = %q, want %q", got.Status, store.Failed)
	}
}

// The agent stops with an error rather than at its own end, and the answer is
// the same: the run gave no report. The error of the run still reaches the
// caller, which writes it to the person.
func TestStartFailsTheTicketWhenTheAgentGivesAnError(t *testing.T) {
	dataDir, id := queuedTicket(t, "Add the thing")

	if err := Start(testfix.OpenStore(t, dataDir), id, fakeAgent(t, "exit 3")); err == nil {
		t.Fatal("err = nil, want the error of the agent")
	}

	if got := testfix.ReadTicket(t, dataDir, id); got.Status != store.Failed {
		t.Errorf("status = %q, want %q", got.Status, store.Failed)
	}
}
