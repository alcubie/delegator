package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/run"
	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

// fakeGit puts a git on the PATH that answers difftool by recording the call
// and runs the real git for every other command, so the checks dg makes before
// the tool still reach git. It returns the path of the record.
func fakeGit(t *testing.T) string {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(t.TempDir(), "difftool")
	bin := t.TempDir()
	script := fmt.Sprintf(`#!/bin/sh
for arg in "$@"; do
	if [ "$arg" = difftool ]; then
		{ pwd -P; printf '%%s\n' "$@"; } > %q
		exit 0
	fi
done
exec %q "$@"
`, record, real)
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return record
}

// difftoolCall reads the working directory and the arguments of the one
// difftool call that the fake git recorded.
func difftoolCall(t *testing.T, record string) (string, []string) {
	t.Helper()
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("the fake git recorded no difftool call: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	return lines[0], lines[1:]
}

// physical resolves every symlink of a path, so the directory that a process
// reports compares equal to the path dg gave it where the temporary directory
// of the test is a symlink.
func physical(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// readyWorktreeIn makes a ready ticket of repo with its worktree on disk, at
// the commit that the default branch holds.
func readyWorktreeIn(t *testing.T, s *store.Store, dataDir, repo, title string) int64 {
	t.Helper()
	id := queuedIn(t, s, repo, title)
	branch := finishIn(t, s, id)
	testfix.GitIn(t, repo, "worktree", "add", run.WorktreePath(dataDir, id), branch)
	return id
}

// readyChangeIn makes a ready ticket whose worktree holds one commit above the
// default branch, which is the ticket dg ticket diff can open.
func readyChangeIn(t *testing.T, s *store.Store, dataDir, repo, title string) int64 {
	t.Helper()
	id := readyWorktreeIn(t, s, dataDir, repo, title)
	commitReadyWork(t, s, dataDir, id)
	return id
}

// wantDifftool is the git command that opens the change of one ticket.
func wantDifftool(dataDir string, id int64) []string {
	return []string{"-C", run.WorktreePath(dataDir, id), "difftool", "-d", repoBranch + "...HEAD"}
}

func TestDiffOpensTheChangeInTheWorktreeOfTheTicket(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	s := testfix.OpenStore(t, dataDir)
	id := readyChangeIn(t, s, dataDir, repo, "the ticket with a change")
	record := fakeGit(t)

	if _, err := runIn(t, dataDir, repo, "ticket", "diff", fmt.Sprint(id)); err != nil {
		t.Fatal(err)
	}

	dir, args := difftoolCall(t, record)
	if want := wantDifftool(dataDir, id); !slices.Equal(args, want) {
		t.Errorf("git arguments = %v, want %v", args, want)
	}
	if want := physical(t, run.WorktreePath(dataDir, id)); physical(t, dir) != want {
		t.Errorf("working directory = %q, want %q", physical(t, dir), want)
	}
}

func TestDiffWithNoIDOpensTheFirstReadyTicketOfTheProject(t *testing.T) {
	dataDir := t.TempDir()
	mine := testfix.Repo(t, repoBranch)
	other := testfix.Repo(t, repoBranch)
	s := testfix.OpenStore(t, dataDir)
	readyChangeIn(t, s, dataDir, other, "the ticket of the other project")
	id := readyChangeIn(t, s, dataDir, mine, "the ticket of this project")
	record := fakeGit(t)

	if _, err := runIn(t, dataDir, mine, "ticket", "diff"); err != nil {
		t.Fatal(err)
	}

	_, args := difftoolCall(t, record)
	if want := wantDifftool(dataDir, id); !slices.Equal(args, want) {
		t.Errorf("git arguments = %v, want %v", args, want)
	}
}

func TestDiffRefusesATicketWithoutAWorktree(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	s := testfix.OpenStore(t, dataDir)
	id := queuedIn(t, s, repo, "the ticket that has not run")

	_, err := runIn(t, dataDir, repo, "ticket", "diff", fmt.Sprint(id))
	if err == nil {
		t.Fatal("dg ticket diff opened a ticket that has no worktree")
	}
	for _, want := range []string{
		fmt.Sprintf("the worktree of ticket %d is gone", id),
		run.WorktreePath(dataDir, id),
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

func TestDiffRefusesADefaultBranchThatDoesNotResolve(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	s := testfix.OpenStore(t, dataDir)
	id := readyChangeIn(t, s, dataDir, repo, "the ticket of a renamed branch")
	testfix.GitIn(t, repo, "branch", "-m", repoBranch, "renamed")

	_, err := runIn(t, dataDir, repo, "ticket", "diff", fmt.Sprint(id))
	if err == nil {
		t.Fatal("dg ticket diff opened a change against a branch that is gone")
	}
	for _, want := range []string{"default branch " + repoBranch, "does not resolve"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

func TestDiffRefusesABranchWithoutACommitAboveTheBase(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	s := testfix.OpenStore(t, dataDir)
	id := readyWorktreeIn(t, s, dataDir, repo, "the ticket that committed nothing")

	_, err := runIn(t, dataDir, repo, "ticket", "diff", fmt.Sprint(id))
	if err == nil {
		t.Fatal("dg ticket diff opened a branch that holds no commit")
	}
	for _, want := range []string{"no commit above " + repoBranch, fmt.Sprintf("ticket %d", id)} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

// Ticket show reports the same answer as the command, so a person who reads
// one of them cannot be told the other.
func TestDiffAvailableGivesTheReasonOfTheCommand(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	s := testfix.OpenStore(t, dataDir)
	open := readyChangeIn(t, s, dataDir, repo, "the ticket with a change")
	refused := readyWorktreeIn(t, s, dataDir, repo, "the ticket that committed nothing")

	available, reason, err := diffAvailable(s, dataDir, open)
	if err != nil {
		t.Fatal(err)
	}
	if !available || reason != "" {
		t.Errorf("diffAvailable(%d) = %t, %q, want true and no reason", open, available, reason)
	}

	available, reason, err = diffAvailable(s, dataDir, refused)
	if err != nil {
		t.Fatal(err)
	}
	if available {
		t.Errorf("diffAvailable(%d) = true, want false", refused)
	}
	_, commandErr := runIn(t, dataDir, repo, "ticket", "diff", fmt.Sprint(refused))
	if commandErr == nil {
		t.Fatal("dg ticket diff opened a branch that holds no commit")
	}
	if reason != commandErr.Error() {
		t.Errorf("reason = %q, want the error of the command %q", reason, commandErr)
	}
}
