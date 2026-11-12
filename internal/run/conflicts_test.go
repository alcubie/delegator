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

	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

// workFile is the file that the work of a ticket changes, and the file the
// default branch changes to overtake it.
const workFile = "ticket-work.txt"

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
	if len(second) != 0 {
		t.Errorf("the second Conflicting = %v, want no ticket", second)
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
