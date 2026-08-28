package project

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initRepo(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init", dir)
	if _, err := cmd.Output(); err != nil {
		t.Fatal(err)
	}
}

func TestRootFindsTheRoot(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	result, err := Root(dir)
	if err != nil {
		t.Fatal(err)
	}

	if result != dir {
		t.Errorf("result = %s, want = %s", result, dir)
	}

	subdir := filepath.Join(dir, "subdir")
	if err := os.Mkdir(subdir, 0o700); err != nil {
		t.Fatal(err)
	}
	result, err = Root(subdir)
	if err != nil {
		t.Fatal(err)
	}
	if result != dir {
		t.Errorf("result = %s, want = %s", result, dir)
	}
}

func TestRootErrorsOutsideARepository(t *testing.T) {
	dir := t.TempDir()
	_, err := Root(dir)
	if !errors.Is(err, ErrNotARepository) {
		t.Errorf("err = %v, want %v", err, ErrNotARepository)
	}
}

func TestRootErrorsWhenGitNotOnPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", "")
	_, err := Root(dir)
	if !errors.Is(err, ErrGitNotOnPath) {
		t.Errorf("err = %v, want %v", err, ErrGitNotOnPath)
	}
}

func TestRootWithDirectoryWithTrailingSpace(t *testing.T) {
	dir := t.TempDir()
	spaceDir := filepath.Join(dir, "dir-with-space ")
	if err := os.Mkdir(spaceDir, 0o700); err != nil {
		t.Fatal(err)
	}

	initRepo(t, spaceDir)
	result, err := Root(spaceDir)
	if err != nil {
		t.Fatal(err)
	}
	if result != spaceDir {
		t.Errorf("result = %s, want = %s", result, spaceDir)
	}
}

// gitIn runs one git command in dir. It stops the test if git gives an error,
// because a repository that the test cannot build is not a result of the test.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	all := append([]string{"-C", dir}, args...)
	if out, err := exec.Command("git", all...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

// commitIn makes one empty commit, because a branch has no ref until a commit
// is on it. The identity is in the command, so the test does not read the
// config of the person.
func commitIn(t *testing.T, dir string) {
	t.Helper()
	gitIn(t, dir, "-c", "user.email=test@example.com", "-c", "user.name=Test",
		"commit", "--allow-empty", "-q", "-m", "first")
}

// trunkRepo makes a repository on a branch that is not main and not master, so
// each result shows which question of DefaultBranch gave the answer.
func trunkRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "trunk")
	return dir
}

func TestDefaultBranchTakesOriginHeadFirst(t *testing.T) {
	dir := trunkRepo(t)
	// A branch with the name main is present, so this shows that the remote
	// comes before it and not only before the branch of HEAD.
	commitIn(t, dir)
	gitIn(t, dir, "branch", "main")
	gitIn(t, dir, "remote", "add", "origin", "https://example.invalid/r.git")
	gitIn(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/develop")

	got, err := DefaultBranch(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != "develop" {
		t.Errorf("DefaultBranch = %q, want %q", got, "develop")
	}
}

func TestDefaultBranchTakesMainBeforeTheBranchOfHead(t *testing.T) {
	dir := trunkRepo(t)
	// A branch has no ref until a commit is on it.
	commitIn(t, dir)
	gitIn(t, dir, "branch", "main")

	got, err := DefaultBranch(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != "main" {
		t.Errorf("DefaultBranch = %q, want %q", got, "main")
	}
}

func TestDefaultBranchTakesMasterWhenThereIsNoMain(t *testing.T) {
	dir := trunkRepo(t)
	commitIn(t, dir)
	gitIn(t, dir, "branch", "master")

	got, err := DefaultBranch(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != "master" {
		t.Errorf("DefaultBranch = %q, want %q", got, "master")
	}
}

func TestDefaultBranchTakesMainBeforeMaster(t *testing.T) {
	dir := trunkRepo(t)
	commitIn(t, dir)
	// master is made first, so an answer of master cannot come from the order
	// of the branches in the repository.
	gitIn(t, dir, "branch", "master")
	gitIn(t, dir, "branch", "main")

	got, err := DefaultBranch(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != "main" {
		t.Errorf("DefaultBranch = %q, want %q", got, "main")
	}
}

func TestDefaultBranchTakesTheBranchOfHeadLast(t *testing.T) {
	// The repository has no remote, no main, no master, and no commit.
	dir := trunkRepo(t)

	got, err := DefaultBranch(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != "trunk" {
		t.Errorf("DefaultBranch = %q, want %q", got, "trunk")
	}
}

// gitLine runs one git command in dir and returns its output with no final
// newline. It stops the test if git gives an error.
func gitLine(t *testing.T, dir string, args ...string) string {
	t.Helper()
	all := append([]string{"-C", dir}, args...)
	out, err := exec.Command("git", all...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func TestFirstCommitGivesTheCommitWithNoParent(t *testing.T) {
	dir := trunkRepo(t)
	commitIn(t, dir)
	want := gitLine(t, dir, "rev-parse", "HEAD")
	// A second commit, so an answer that comes from HEAD is not the same as an
	// answer that comes from the first commit.
	commitIn(t, dir)

	got, err := FirstCommit(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("FirstCommit = %q, want %q", got, want)
	}
}

func TestFirstCommitWhenTheRepositoryHasNoCommit(t *testing.T) {
	dir := trunkRepo(t)

	_, err := FirstCommit(dir)
	if !errors.Is(err, ErrNoCommit) {
		t.Errorf("err = %v, want %v", err, ErrNoCommit)
	}
}

// commitAt makes one empty commit with a known date. rev-list gives the newest
// commit first, so the date controls the sequence of its result.
func commitAt(t *testing.T, dir, date string) {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "-c", "user.email=test@example.com",
		"-c", "user.name=Test", "commit", "--allow-empty", "-q", "-m", "commit")
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, out)
	}
}

// A merge of two histories that had no relation gives a repository two commits
// with no parent. The older one is the start of the repository, and a history
// that a person adds later must not change the answer.
func TestFirstCommitWithTwoHistoriesGivesTheOlder(t *testing.T) {
	dir := trunkRepo(t)
	commitAt(t, dir, "2020-01-01T00:00:00Z")
	want := gitLine(t, dir, "rev-parse", "HEAD")

	// An orphan branch starts a history that has no relation to the first one.
	gitIn(t, dir, "checkout", "-q", "--orphan", "second")
	commitAt(t, dir, "2021-01-01T00:00:00Z")
	gitIn(t, dir, "checkout", "-q", "trunk")
	gitIn(t, dir, "-c", "user.email=test@example.com", "-c", "user.name=Test",
		"merge", "-q", "--allow-unrelated-histories", "-m", "merge", "second")

	got, err := FirstCommit(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("FirstCommit = %q, want %q", got, want)
	}
}
