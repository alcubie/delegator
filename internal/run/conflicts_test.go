package run

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

// workFile is the file that the work of a ticket changes, and the file the
// default branch changes to overtake it.
const workFile = "ticket-work.txt"

// conflictConfig leaves the runs of the tests alone and has room for one more.
var conflictConfig = config.Config{Runs: 1, TimeoutMinutes: 60, ResolveConflicts: true}

// withoutConflictRuns is conflictConfig with the setting that turns the
// conflict runs off.
func withoutConflictRuns() config.Config {
	cfg := conflictConfig
	cfg.ResolveConflicts = false
	return cfg
}

// noStarts is the queue launcher of a Reconcile that must start nothing.
func noStarts(t *testing.T) func() *exec.Cmd {
	t.Helper()
	return func() *exec.Cmd {
		t.Error("a queued ticket started, want none")
		return exec.Command("true")
	}
}

// recordingConflicts gives the conflict launcher of a Reconcile and the ids it
// was asked to start, in order.
func recordingConflicts() (func(int64) *exec.Cmd, func() []int64) {
	var started []int64
	return func(id int64) *exec.Cmd {
		started = append(started, id)
		return exec.Command("true")
	}, func() []int64 { return started }
}

// readyAfterARun leaves a ticket as a run of agent leaves it: ready, holding a
// session of that agent and the commit it reported.
func readyAfterARun(t *testing.T, s *store.Store, dataDir string, id int64, agent, session, commit string) {
	t.Helper()
	agentID, err := s.AgentID(agent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(id, branch(id, "a title"), agentID); err != nil {
		t.Fatal(err)
	}
	testfix.SetSession(t, dataDir, id, session)
	if err := s.FinishTicket(id, commit, ""); err != nil {
		t.Fatal(err)
	}
}

// countingGit puts a git on the PATH that records each call and runs the real
// git, so the checks that reached Git are countable. It returns the number of
// calls of one subcommand.
func countingGit(t *testing.T) func(subcommand string) int {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "calls")
	bin := t.TempDir()
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %q
exec %q "$@"
`, log, real)
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	return func(subcommand string) int {
		t.Helper()
		data, err := os.ReadFile(log)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			t.Fatal(err)
		}
		calls := 0
		for line := range strings.SplitSeq(string(data), "\n") {
			if slices.Contains(strings.Fields(line), subcommand) {
				calls++
			}
		}
		return calls
	}
}

// readyStore makes a repository on main and a store that holds it as a
// project, and returns the store and the repository.
func readyStore(t *testing.T) (*store.Store, string) {
	t.Helper()
	repo := testfix.Repo(t, "main")
	s := testfix.OpenStore(t, t.TempDir())
	if _, err := s.ProjectID(repo, "main"); err != nil {
		t.Fatal(err)
	}
	return s, repo
}

// queuedIn adds a queued ticket of repo, registering the project on its first
// use.
func queuedIn(t *testing.T, s *store.Store, repo, title string) int64 {
	t.Helper()
	projectID, err := s.ProjectID(repo, "main")
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.AddTicket(projectID, title)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// readyIn makes a ready ticket of repo whose work is one commit of workFile on
// a branch of its own. The default branch stays checked out, as it is while
// the person reviews the work of a run.
func readyIn(t *testing.T, s *store.Store, repo, title string) int64 {
	t.Helper()
	id := queuedIn(t, s, repo, title)
	branch := fmt.Sprintf("delegator/%d-a-title", id)
	testfix.GitIn(t, repo, "checkout", "-q", "-b", branch)
	testfix.CommitFileIn(t, repo, workFile, fmt.Sprintf("the work of ticket %d\n", id), "ticket work")
	commit := testfix.GitOut(t, repo, "rev-parse", "HEAD")
	testfix.GitIn(t, repo, "checkout", "-q", "main")
	finishIn(t, s, id, commit)
	return id
}

// finishIn claims a queued ticket and reports commit for it, which is the
// ready state that a run leaves behind.
func finishIn(t *testing.T, s *store.Store, id int64, commit string) {
	t.Helper()
	if _, err := s.Claim(id, fmt.Sprintf("delegator/%d-a-title", id), testAgentID); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishTicket(id, commit, ""); err != nil {
		t.Fatal(err)
	}
}

// overtake puts the change of another ticket on the default branch, over the
// file that the work of the ready ticket holds.
func overtake(t *testing.T, repo string) {
	t.Helper()
	testfix.CommitFileIn(t, repo, workFile, "the change of another ticket\n", "another ticket landed")
}

// checkedBase reads the commit of the default branch stored for one ticket.
func checkedBase(t *testing.T, s *store.Store, id int64) string {
	t.Helper()
	ready, err := s.ReadyMerges()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ready {
		for _, ticket := range p.Tickets {
			if ticket.ID == id {
				return ticket.CheckedBase
			}
		}
	}
	t.Fatalf("ticket %d is not among the ready tickets that hold a commit", id)
	return ""
}

// storedVerdict reads the verdict stored for one ticket, nil when none is.
func storedVerdict(t *testing.T, s *store.Store, id int64) *bool {
	t.Helper()
	ready, err := s.ReadyMerges()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ready {
		for _, ticket := range p.Tickets {
			if ticket.ID == id {
				return ticket.MergesCleanly
			}
		}
	}
	t.Fatalf("ticket %d is not among the ready tickets that hold a commit", id)
	return nil
}

func wantVerdict(t *testing.T, got *bool, want bool) {
	t.Helper()
	if got == nil || *got != want {
		t.Errorf("the stored verdict is %v, want %v", got, want)
	}
}

// The person finds the conflict today when they merge the branch of the
// ticket. The check finds it while the ticket waits for review instead.
func TestConflictingFindsTheTicketTheDefaultBranchOvertook(t *testing.T) {
	s, repo := readyStore(t)
	id := readyIn(t, s, repo, "the ticket the default branch overtook")
	overtake(t, repo)

	got, err := Conflicting(s)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []int64{id}) {
		t.Errorf("Conflicting = %v, want %v", got, []int64{id})
	}
}

// Every dg command runs the check, so a conflict that arrived once must not
// arrive again until the branch moves, and must not pay for Git again either.
func TestConflictingChecksATicketOnceForOneCommitOfTheDefaultBranch(t *testing.T) {
	s, repo := readyStore(t)
	id := readyIn(t, s, repo, "the ticket the default branch overtook")
	overtake(t, repo)
	calls := countingGit(t)

	first, err := Conflicting(s)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(first, []int64{id}) {
		t.Fatalf("the first Conflicting = %v, want %v", first, []int64{id})
	}

	second, err := Conflicting(s)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(second, []int64{id}) {
		t.Errorf("the second Conflicting = %v, want %v from the stored verdict", second, []int64{id})
	}
	if got := calls("merge-tree"); got != 1 {
		t.Errorf("the check ran %d merge-tree calls, want 1", got)
	}
}

// A ticket that conflicts with one commit of the default branch can merge into
// the next, and the person needs the answer for the branch as it is now.
func TestConflictingChecksTheTicketAgainAfterTheDefaultBranchMoves(t *testing.T) {
	s, repo := readyStore(t)
	id := readyIn(t, s, repo, "the ticket the default branch overtook")
	overtake(t, repo)
	if _, err := Conflicting(s); err != nil {
		t.Fatal(err)
	}
	testfix.CommitIn(t, repo, "one more commit of the default branch")

	got, err := Conflicting(s)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []int64{id}) {
		t.Errorf("Conflicting = %v after the branch moved again, want %v", got, []int64{id})
	}
}

// A ticket rerun under the same session can report a new commit while the
// default branch stands still. FinishTicket must clear the stored base, or
// the cache that keeps one check per move of the branch would read as
// "already checked" for a commit nothing checked.
func TestConflictingChecksTheTicketAgainAfterANewCommitWhileTheBaseStands(t *testing.T) {
	s, repo := readyStore(t)
	id := readyIn(t, s, repo, "the ticket that reports again")
	overtake(t, repo)
	if _, err := Conflicting(s); err != nil {
		t.Fatal(err)
	}

	testfix.GitIn(t, repo, "checkout", "-q", branch(id, "a title"))
	testfix.CommitFileIn(t, repo, workFile, fmt.Sprintf("the corrected work of ticket %d\n", id), "ticket work, again")
	commit := testfix.GitOut(t, repo, "rev-parse", "HEAD")
	testfix.GitIn(t, repo, "checkout", "-q", "main")
	if err := s.FinishTicket(id, commit, ""); err != nil {
		t.Fatal(err)
	}

	got, err := Conflicting(s)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []int64{id}) {
		t.Errorf("Conflicting = %v, want %v", got, []int64{id})
	}
}

// The ordinary ticket still lands on top of the branch, and storing the commit
// it was checked against is what keeps the next command from checking it
// again.
func TestConflictingLeavesTheTicketThatStillMerges(t *testing.T) {
	s, repo := readyStore(t)
	id := readyIn(t, s, repo, "the ticket that still lands")
	testfix.CommitFileIn(t, repo, "other.txt", "the change of another ticket\n", "another ticket landed")

	got, err := Conflicting(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("Conflicting = %v, want no ticket", got)
	}
	if got, want := checkedBase(t, s, id), testfix.GitOut(t, repo, "rev-parse", "main"); got != want {
		t.Errorf("the stored commit is %q, want %q", got, want)
	}
}

// Only a ready ticket holds work waiting to be merged. A run in flight moves
// its branch, and a done ticket is already in the default branch.
func TestConflictingChecksOnlyAReadyTicket(t *testing.T) {
	s, repo := readyStore(t)
	queuedIn(t, s, repo, "the ticket in the queue")
	running := readyIn(t, s, repo, "the ticket of a run in flight")
	if _, err := s.Rerun(running, testAgentID, 4242); err != nil {
		t.Fatal(err)
	}
	done := readyIn(t, s, repo, "the ticket the person accepted")
	if err := s.ChangeStatus(done, store.Done); err != nil {
		t.Fatal(err)
	}
	overtake(t, repo)
	calls := countingGit(t)

	got, err := Conflicting(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("Conflicting = %v, want no ticket", got)
	}
	if got := calls("merge-tree"); got != 0 {
		t.Errorf("the check ran %d merge-tree calls for tickets outside ready, want 0", got)
	}
}

// An archived project is out of the work of the person, so its ready tickets
// must not cost a Git process on every command.
func TestConflictingSkipsAnArchivedProject(t *testing.T) {
	s, repo := readyStore(t)
	readyIn(t, s, repo, "the ticket of the archived project")
	overtake(t, repo)
	if _, err := s.ArchiveProject(repo); err != nil {
		t.Fatal(err)
	}
	calls := countingGit(t)

	got, err := Conflicting(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("Conflicting = %v, want no ticket", got)
	}
	if got := calls("rev-parse"); got != 0 {
		t.Errorf("the check ran %d rev-parse calls for an archived project, want 0", got)
	}
}

// dg runs the check on every command, including the commands of a person who
// has no ready ticket and no git.
func TestConflictingWithoutGitAndWithNoReadyTicket(t *testing.T) {
	s, repo := readyStore(t)
	queuedIn(t, s, repo, "the ticket in the queue")
	t.Setenv("PATH", t.TempDir())

	got, err := Conflicting(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("Conflicting = %v, want no ticket", got)
	}
}

// One commit of the default branch overtakes every ticket that holds the file
// it changed, so the person hears about each of them.
func TestConflictingFindsEveryOvertakenTicketOfAProject(t *testing.T) {
	s, repo := readyStore(t)
	first := readyIn(t, s, repo, "the first ticket")
	second := readyIn(t, s, repo, "the second ticket")
	overtake(t, repo)

	got, err := Conflicting(s)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []int64{first, second}) {
		t.Errorf("Conflicting = %v, want %v", got, []int64{first, second})
	}
}

// The default branch of a project is read once however many of its tickets
// wait for review. The tickets of this project merge, which is the answer
// merge-tree gives without reading a revision of its own.
func TestConflictingReadsTheDefaultBranchOnceForAProject(t *testing.T) {
	s, repo := readyStore(t)
	readyIn(t, s, repo, "the first ticket")
	readyIn(t, s, repo, "the second ticket")
	testfix.CommitFileIn(t, repo, "other.txt", "the change of another ticket\n", "another ticket landed")
	calls := countingGit(t)

	got, err := Conflicting(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("Conflicting = %v, want no ticket", got)
	}
	if got := calls("merge-tree"); got != 2 {
		t.Errorf("the check ran %d merge-tree calls, want one for each ticket", got)
	}
	if got := calls("rev-parse"); got != 1 {
		t.Errorf("the check ran %d rev-parse calls for one project, want 1", got)
	}
}

// A run can reach ready without reporting a commit. There is nothing to merge
// for that ticket, and it must not take the check of the ticket beside it with
// it.
func TestConflictingPassesOverAReadyTicketWithoutACommit(t *testing.T) {
	s, repo := readyStore(t)
	finishIn(t, s, queuedIn(t, s, repo, "the ticket that reported no commit"), "")
	overtaken := readyIn(t, s, repo, "the ticket the default branch overtook")
	overtake(t, repo)

	got, err := Conflicting(s)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []int64{overtaken}) {
		t.Errorf("Conflicting = %v, want %v", got, []int64{overtaken})
	}
}

// A repository the person moved or removed must not break every dg command,
// and must not hide the conflicts of the other projects.
func TestConflictingSkipsAProjectWhosePathIsGone(t *testing.T) {
	s, gone := readyStore(t)
	readyIn(t, s, gone, "the ticket of the project that moved")
	overtake(t, gone)
	kept := testfix.Repo(t, "main")
	id := readyIn(t, s, kept, "the ticket of the project that is there")
	overtake(t, kept)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}

	got, err := Conflicting(s)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []int64{id}) {
		t.Errorf("Conflicting = %v, want %v", got, []int64{id})
	}
}

// A commit that Git cannot read is a failure and not an answer, so the ticket
// keeps no commit of the default branch and the check of the next project
// still runs.
func TestConflictingSkipsAProjectWhoseTicketCommitIsGone(t *testing.T) {
	s, repo := readyStore(t)
	unreadable := queuedIn(t, s, repo, "the ticket whose commit is gone")
	finishIn(t, s, unreadable, "0123456789012345678901234567890123456789")
	kept := testfix.Repo(t, "main")
	id := readyIn(t, s, kept, "the ticket of the other project")
	overtake(t, kept)

	got, err := Conflicting(s)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []int64{id}) {
		t.Errorf("Conflicting = %v, want %v", got, []int64{id})
	}
	if got := checkedBase(t, s, unreadable); got != "" {
		t.Errorf("the ticket whose commit is gone stored %q, want no commit", got)
	}
}

// Today the person opens a chat with the agent and asks for the merge. The
// next command of any kind starts that run instead.
func TestReconcileStartsARunForTheOvertakenTicket(t *testing.T) {
	s, repo := readyStore(t)
	id := readyIn(t, s, repo, "the ticket the default branch overtook")
	overtake(t, repo)
	launch, started := recordingConflicts()

	if err := Reconcile(s, noStarts(t), launch, conflictConfig); err != nil {
		t.Fatal(err)
	}

	if got := started(); !slices.Equal(got, []int64{id}) {
		t.Errorf("the conflict runs started for %v, want %v", got, []int64{id})
	}
}

// A branch that is only behind the default branch holds no work for the agent,
// and the person can accept it as it is.
func TestReconcileLeavesTheTicketThatStillMerges(t *testing.T) {
	s, repo := readyStore(t)
	id := readyIn(t, s, repo, "the ticket that still lands")
	testfix.CommitFileIn(t, repo, "other.txt", "the change of another ticket\n", "another ticket landed")
	launch, started := recordingConflicts()

	if err := Reconcile(s, noStarts(t), launch, conflictConfig); err != nil {
		t.Fatal(err)
	}

	if got := started(); len(got) != 0 {
		t.Errorf("the conflict runs started for %v, want none", got)
	}
	if got, err := s.Ticket(id); err != nil || got.Status != store.Ready {
		t.Errorf("the ticket is %q, %v; want %q", got.Status, err, store.Ready)
	}
}

// Every command runs the check, and the supervisor it starts reaches the
// transition a moment later. Two commands in that moment must not give one
// ticket two runs.
func TestReconcileStartsOneRunForOneCommitOfTheDefaultBranch(t *testing.T) {
	s, repo := readyStore(t)
	id := readyIn(t, s, repo, "the ticket the default branch overtook")
	overtake(t, repo)
	launch, started := recordingConflicts()

	for range 2 {
		if err := Reconcile(s, noStarts(t), launch, conflictConfig); err != nil {
			t.Fatal(err)
		}
	}

	if got := started(); !slices.Equal(got, []int64{id}) {
		t.Errorf("the conflict runs started for %v, want %v", got, []int64{id})
	}
}

// A paused queue starts no agent. The check must also store nothing while
// paused, or the commit it checked against would be the only one for which the
// run never happens.
func TestReconcileStartsNoRunWhileTheQueueIsPaused(t *testing.T) {
	s, repo := readyStore(t)
	id := readyIn(t, s, repo, "the ticket the default branch overtook")
	overtake(t, repo)
	if err := s.PauseQueue(); err != nil {
		t.Fatal(err)
	}
	launch, started := recordingConflicts()

	if err := Reconcile(s, noStarts(t), launch, conflictConfig); err != nil {
		t.Fatal(err)
	}
	if got := started(); len(got) != 0 {
		t.Errorf("the conflict runs started for %v while the queue was paused, want none", got)
	}
	if got, err := s.Ticket(id); err != nil || got.Status != store.Ready {
		t.Errorf("the paused ticket is %q, %v; want %q", got.Status, err, store.Ready)
	}

	if err := s.ResumeQueue(); err != nil {
		t.Fatal(err)
	}
	if err := Reconcile(s, noStarts(t), launch, conflictConfig); err != nil {
		t.Fatal(err)
	}
	if got := started(); !slices.Equal(got, []int64{id}) {
		t.Errorf("the conflict runs started for %v after the queue started again, want %v", got, []int64{id})
	}
}

// Each conflict run spends tokens, and a person who would rather merge the
// branch themselves turns them off with resolve_conflicts. The check is then
// worth nothing to them, so it must not start a Git process either, and it
// must not store the commit it would have checked against: the run has to
// start the moment they turn the setting back on.
func TestReconcileStartsNoRunWhileResolveConflictsIsOff(t *testing.T) {
	s, repo := readyStore(t)
	id := readyIn(t, s, repo, "the ticket the default branch overtook")
	overtake(t, repo)
	launch, started := recordingConflicts()
	calls := countingGit(t)

	if err := Reconcile(s, noStarts(t), launch, withoutConflictRuns()); err != nil {
		t.Fatal(err)
	}
	if got := started(); len(got) != 0 {
		t.Errorf("the conflict runs started for %v while the setting was off, want none", got)
	}
	if got, err := s.Ticket(id); err != nil || got.Status != store.Ready {
		t.Errorf("the ticket is %q, %v; want %q", got.Status, err, store.Ready)
	}
	if got := calls("rev-parse"); got != 0 {
		t.Errorf("the check ran %d rev-parse calls while the setting was off, want 0", got)
	}

	if err := Reconcile(s, noStarts(t), launch, conflictConfig); err != nil {
		t.Fatal(err)
	}
	if got := started(); !slices.Equal(got, []int64{id}) {
		t.Errorf("the conflict runs started for %v after the setting went on, want %v", got, []int64{id})
	}
}

func TestReconcileStartsARunForAConflictTheInboxCheckedWhileTheQueueWasPaused(t *testing.T) {
	s, repo := readyStore(t)
	id := readyIn(t, s, repo, "the ticket the default branch overtook")
	if err := s.PauseQueue(); err != nil {
		t.Fatal(err)
	}
	overtake(t, repo)
	if _, err := MergeAnswers(s); err != nil {
		t.Fatal(err)
	}
	if err := s.ResumeQueue(); err != nil {
		t.Fatal(err)
	}
	launch, started := recordingConflicts()

	if err := Reconcile(s, noStarts(t), launch, conflictConfig); err != nil {
		t.Fatal(err)
	}

	if got := started(); !slices.Equal(got, []int64{id}) {
		t.Errorf("the conflict runs started for %v, want %v", got, []int64{id})
	}
}

func TestReconcileStartsARunForAConflictTheInboxCheckedWhileResolveConflictsWasOff(t *testing.T) {
	s, repo := readyStore(t)
	id := readyIn(t, s, repo, "the ticket the default branch overtook")
	overtake(t, repo)
	launch, started := recordingConflicts()
	if err := Reconcile(s, noStarts(t), launch, withoutConflictRuns()); err != nil {
		t.Fatal(err)
	}
	if _, err := MergeAnswers(s); err != nil {
		t.Fatal(err)
	}

	if err := Reconcile(s, noStarts(t), launch, conflictConfig); err != nil {
		t.Fatal(err)
	}

	if got := started(); !slices.Equal(got, []int64{id}) {
		t.Errorf("the conflict runs started for %v, want %v", got, []int64{id})
	}
}

func TestReconcileStartsOneRunForAConflictTheInboxCheckedFirst(t *testing.T) {
	s, repo := readyStore(t)
	id := readyIn(t, s, repo, "the ticket the default branch overtook")
	overtake(t, repo)
	if _, err := MergeAnswers(s); err != nil {
		t.Fatal(err)
	}
	launch, started := recordingConflicts()

	for range 2 {
		if err := Reconcile(s, noStarts(t), launch, conflictConfig); err != nil {
			t.Fatal(err)
		}
	}

	if got := started(); !slices.Equal(got, []int64{id}) {
		t.Errorf("the conflict runs started for %v, want %v", got, []int64{id})
	}
}

// A conflict run starts a new session, because the session that reported the
// commit can hold a very large context. The fake agent replays history for a
// loaded session and not for a new one, and a new session gets a new ID.
func TestConflictStartsANewSessionForTheTicket(t *testing.T) {
	dataDir, id := queuedTicket(t, "a title")
	sent := filepath.Join(t.TempDir(), "prompt")
	replayed := filepath.Join(t.TempDir(), "replayed")
	cfg := acpConfig(t, dataDir, "prompt "+sent, "stop end_turn",
		"history:", "write "+replayed+" replayed")
	s := testfix.OpenStore(t, dataDir)
	readyAfterARun(t, s, dataDir, id, "fake", "fake-7", "")
	first, err := s.Run(id)
	if err != nil {
		t.Fatal(err)
	}

	if err := Conflict(s, id, cfg); err != nil {
		t.Fatal(err)
	}

	got := testfix.WaitFor(t, sent)
	if _, err := os.Stat(replayed); err == nil {
		t.Error("the conflict run loaded the session of the ticket")
	}
	ticket, err := s.Ticket(id)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Session == "" || ticket.Session == "fake-7" {
		t.Errorf("the session of the ticket is %q, want a new session", ticket.Session)
	}
	for _, want := range []string{
		fmt.Sprintf("delegator ticket %d,", id),
		"no longer merges into main",
		fmt.Sprintf("dg ticket show %d --data-dir %q", id, dataDir),
		"git log main..HEAD",
		"Merge main into this branch. Merge it; do not rebase.",
		"do not amend it",
		fmt.Sprintf("dg ticket finish %d <hash> --data-dir %q", id, dataDir),
		"cache directory",
		"How to read the repository:",
		fmt.Sprintf("dg ticket rerun %d", id),
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the prompt the agent received does not hold %q:\n%s", want, got)
		}
	}
	second, err := s.Run(id)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID {
		t.Error("the conflict run reused the run that reported the commit")
	}
	if second.PID != os.Getpid() {
		t.Errorf("the conflict run records PID %d, want this supervisor at %d", second.PID, os.Getpid())
	}
}

// Only dg ticket finish makes work ready. A conflict run that ends without it
// leaves the ticket failed, as any other run does, and a failed ticket is out
// of the check, so nothing tries again on its own.
func TestConflictLeavesTheTicketFailedWhenTheRunReportsNothing(t *testing.T) {
	dataDir, id := queuedTicket(t, "a title")
	cfg := acpConfig(t, dataDir, "stop end_turn")
	s := testfix.OpenStore(t, dataDir)
	readyAfterARun(t, s, dataDir, id, "fake", "fake-7", "")

	if err := Conflict(s, id, cfg); err != nil {
		t.Fatal(err)
	}

	if got := testfix.ReadTicket(t, dataDir, id).Status; got != store.Failed {
		t.Errorf("the ticket is %q, want %q", got, store.Failed)
	}
}

// Only a ready ticket has a commit waiting for review to correct.
func TestConflictRefusesATicketThatIsNotReady(t *testing.T) {
	dataDir, id := queuedTicket(t, "a title")
	cfg := acpConfig(t, dataDir, "stop end_turn")
	s := testfix.OpenStore(t, dataDir)

	err := Conflict(s, id, cfg)

	if !errors.Is(err, store.ErrInvalidTicketStateChange) {
		t.Errorf("err = %v, want ErrInvalidTicketStateChange", err)
	}
}

func TestConflictingStoresFalseForAConflictingTicket(t *testing.T) {
	s, repo := readyStore(t)
	id := readyIn(t, s, repo, "the ticket the default branch overtook")
	overtake(t, repo)
	if _, err := Conflicting(s); err != nil {
		t.Fatal(err)
	}
	wantVerdict(t, storedVerdict(t, s, id), false)
}

func TestConflictingStoresTrueForACleanTicket(t *testing.T) {
	s, repo := readyStore(t)
	id := readyIn(t, s, repo, "the ticket that still lands")
	testfix.CommitFileIn(t, repo, "other.txt", "the change of another ticket\n", "another ticket landed")
	if _, err := Conflicting(s); err != nil {
		t.Fatal(err)
	}
	wantVerdict(t, storedVerdict(t, s, id), true)
}

func TestReadyMergesReportsNoVerdictForAnUncheckedTicket(t *testing.T) {
	s, repo := readyStore(t)
	id := readyIn(t, s, repo, "the ticket nothing checked")
	if got := storedVerdict(t, s, id); got != nil {
		t.Errorf("the stored verdict is %v, want none", *got)
	}
}

func TestFinishTicketClearsTheStoredVerdict(t *testing.T) {
	s, repo := readyStore(t)
	id := readyIn(t, s, repo, "the ticket that reports again")
	overtake(t, repo)
	if _, err := Conflicting(s); err != nil {
		t.Fatal(err)
	}
	commit := testfix.GitOut(t, repo, "rev-parse", branch(id, "a title"))
	if err := s.FinishTicket(id, commit, ""); err != nil {
		t.Fatal(err)
	}
	if got := storedVerdict(t, s, id); got != nil {
		t.Errorf("the stored verdict is %v after a new commit, want none", *got)
	}
}

func TestConflictingReturnsTheStoredConflictWithoutAGitCheck(t *testing.T) {
	s, repo := readyStore(t)
	id := readyIn(t, s, repo, "the ticket the default branch overtook")
	overtake(t, repo)
	if _, err := Conflicting(s); err != nil {
		t.Fatal(err)
	}
	calls := countingGit(t)

	got, err := Conflicting(s)
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(got, []int64{id}) {
		t.Errorf("Conflicting = %v, want %v", got, []int64{id})
	}
	if n := calls("merge-tree"); n != 0 {
		t.Errorf("the stored verdict cost %d merge-tree calls, want 0", n)
	}
}

func TestMergeAnswersChecksATicketWhoseBaseMovedAndStoresTheAnswer(t *testing.T) {
	s, repo := readyStore(t)
	id := readyIn(t, s, repo, "the ticket the default branch overtook")
	overtake(t, repo)

	got, err := MergeAnswers(s)
	if err != nil {
		t.Fatal(err)
	}

	wantVerdict(t, got[id], false)
	wantVerdict(t, storedVerdict(t, s, id), false)
}

func TestMergeAnswersGivesNilForAProjectWhosePathIsGone(t *testing.T) {
	s, repo := readyStore(t)
	id := readyIn(t, s, repo, "the ticket of the lost project")
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}

	got, err := MergeAnswers(s)
	if err != nil {
		t.Fatal(err)
	}

	if answer, ok := got[id]; !ok || answer != nil {
		t.Errorf("the answer is %v, present %v; want nil", answer, ok)
	}
}
