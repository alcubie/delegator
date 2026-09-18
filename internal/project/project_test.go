package project

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These fixtures start git through Command rather than exec.Command, because
// Command removes GIT_DIR, GIT_INDEX_FILE and the other gitEnv variables from
// the environment. Git sets those variables when it runs a hook, and the
// pre-commit hook runs make check, so every test inherits them. When GIT_DIR
// is set, git works on the repository it names, whatever directory -C gives
// it. A fixture that meant to build a temporary repository would then run its
// git init and git commit inside the real repository of the person.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	if _, err := Command(dir, "init").Output(); err != nil {
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

func TestRootOfALinkedWorktreeFindsThePrimaryCheckout(t *testing.T) {
	primary := t.TempDir()
	initRepo(t, primary)
	commitIn(t, primary)
	linked := filepath.Join(t.TempDir(), "linked")
	gitIn(t, primary, "worktree", "add", "-b", "linked", linked)
	t.Cleanup(func() { gitIn(t, primary, "worktree", "remove", "--force", linked) })

	got, err := Root(linked)
	if err != nil {
		t.Fatal(err)
	}
	if got != primary {
		t.Errorf("Root(linked worktree) = %q, want primary checkout %q", got, primary)
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
	if out, err := Command(dir, args...).CombinedOutput(); err != nil {
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
	out, err := Command(dir, args...).Output()
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
// commit first, so the date controls the order of its result.
func commitAt(t *testing.T, dir, date string) {
	t.Helper()
	cmd := Command(dir, "-c", "user.email=test@example.com",
		"-c", "user.name=Test", "commit", "--allow-empty", "-q", "-m", "commit")
	cmd.Env = append(cmd.Env, "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
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

// A hook of git sets GIT_INDEX_FILE and GIT_DIR to a path that is relative to
// the root of the repository. A command of delegator runs in a different
// directory, so git must not see either value.
func TestRootIgnoresTheGitEnvironmentOfTheCaller(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	sub := filepath.Join(dir, "subdir")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_DIR", ".git")
	t.Setenv("GIT_INDEX_FILE", ".git/index")

	got, err := Root(sub)
	if err != nil {
		t.Fatal(err)
	}
	if got != dir {
		t.Errorf("root = %s, want %s", got, dir)
	}
}

// A git command that fails writes its reason to stderr, and the error of
// gitOutput is the only place the person can read it.
func TestGitOutputGivesTheStderrOfGit(t *testing.T) {
	dir := trunkRepo(t)

	_, err := gitOutput(dir, "rev-parse", "--verify", "nosuchref")
	if err == nil {
		t.Fatal("err = nil, want an error")
	}
	if !strings.Contains(err.Error(), "Needed a single revision") {
		t.Errorf("err = %v, want the text git wrote", err)
	}
	if strings.Contains(err.Error(), "\n") {
		t.Errorf("err = %q, want no newline from the end of the stderr", err)
	}
}

// Git says no with nothing on stderr when it is asked to be quiet. The error
// then has the exit status and no more, and it must still say something.
func TestGitOutputWithNoStderrStillGivesAnError(t *testing.T) {
	dir := trunkRepo(t)

	_, err := gitOutput(dir, "rev-parse", "--verify", "--quiet", "refs/heads/nope")
	if err == nil {
		t.Fatal("err = nil, want an error")
	}
	if err.Error() == "" {
		t.Error("err.Error() = empty, want the text of the exit status")
	}
	if strings.HasPrefix(err.Error(), ":") {
		t.Errorf("err = %q, want no empty stderr in front of the status", err)
	}
}

// Callers of gitOutput read the failure of git from the type of the error, so
// the exec.ExitError has to survive the wrapping with its exit code.
func TestGitOutputKeepsTheExitError(t *testing.T) {
	dir := trunkRepo(t)

	_, err := gitOutput(dir, "rev-parse", "--verify", "nosuchref")
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("err = %v, want an *exec.ExitError under it", err)
	}
	if exitErr.ExitCode() != 128 {
		t.Errorf("exit code = %d, want 128", exitErr.ExitCode())
	}
}

// commitFile writes and commits one file on the branch that is checked out.
func commitFile(t *testing.T, dir, name, content, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", name)
	gitIn(t, dir, "-c", "user.email=test@example.com", "-c", "user.name=Test",
		"commit", "-q", "-m", message)
}

// mergedBranch makes a branch with one content change and leaves trunk checked
// out. A test can then choose how, or whether, to put that change into HEAD.
func mergedBranch(t *testing.T) (string, string) {
	t.Helper()
	dir := trunkRepo(t)
	commitIn(t, dir)
	gitIn(t, dir, "checkout", "-q", "-b", "ticket")
	commitFile(t, dir, "ticket.txt", "the complete ticket\n", "ticket work")
	gitIn(t, dir, "checkout", "-q", "trunk")
	return dir, "ticket"
}

func TestRequireBranchMergedAcceptsAnOrdinaryMerge(t *testing.T) {
	dir, branch := mergedBranch(t)
	gitIn(t, dir, "-c", "user.email=test@example.com", "-c", "user.name=Test",
		"merge", "-q", "--no-ff", "-m", "merge ticket", branch)

	if err := RequireBranchMerged(dir, branch); err != nil {
		t.Fatal(err)
	}
}

func TestRequireBranchMergedRefusesAnUnmergedBranch(t *testing.T) {
	dir, branch := mergedBranch(t)

	err := RequireBranchMerged(dir, branch)
	if !errors.Is(err, ErrBranchNotMerged) {
		t.Fatalf("err = %v, want ErrBranchNotMerged", err)
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("err = %q, want the force override", err)
	}
}

func TestRequireBranchMergedAcceptsAnEquivalentSquash(t *testing.T) {
	dir, branch := mergedBranch(t)
	gitIn(t, dir, "checkout", "-q", branch)
	commitFile(t, dir, "second.txt", "the other part\n", "more ticket work")
	gitIn(t, dir, "checkout", "-q", "trunk")
	gitIn(t, dir, "merge", "-q", "--squash", "--ff", branch)
	gitIn(t, dir, "-c", "user.email=test@example.com", "-c", "user.name=Test",
		"commit", "-q", "-m", "squash ticket")

	if err := RequireBranchMerged(dir, branch); err != nil {
		t.Fatal(err)
	}
}

func TestRequireBranchMergedRefusesOnlyPartOfTheChange(t *testing.T) {
	dir, branch := mergedBranch(t)
	gitIn(t, dir, "checkout", "-q", branch)
	commitFile(t, dir, "second.txt", "the other part\n", "more ticket work")
	gitIn(t, dir, "checkout", "-q", "trunk")
	commitFile(t, dir, "ticket.txt", "the complete ticket\n", "only one part")

	if err := RequireBranchMerged(dir, branch); !errors.Is(err, ErrBranchNotMerged) {
		t.Fatalf("err = %v, want ErrBranchNotMerged", err)
	}
}

func TestRequireBranchMergedRefusesTheChangeBundledWithOtherWork(t *testing.T) {
	dir, branch := mergedBranch(t)
	if err := os.WriteFile(filepath.Join(dir, "ticket.txt"), []byte("the complete ticket\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.txt"), []byte("unrelated\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "ticket.txt", "other.txt")
	gitIn(t, dir, "-c", "user.email=test@example.com", "-c", "user.name=Test",
		"commit", "-q", "-m", "ticket and other work")

	if err := RequireBranchMerged(dir, branch); !errors.Is(err, ErrBranchNotMerged) {
		t.Fatalf("err = %v, want ErrBranchNotMerged", err)
	}
}

// Exit status 1 from --is-ancestor means only that the branch is not an
// ancestor. Every other Git failure must stay distinguishable from that normal
// negative answer.
func TestRequireBranchMergedPreservesOtherGitFailures(t *testing.T) {
	dir := trunkRepo(t)
	commitIn(t, dir)

	err := RequireBranchMerged(dir, "missing")
	if errors.Is(err, ErrBranchNotMerged) {
		t.Fatalf("err = %v, want the Git failure", err)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 128 {
		t.Fatalf("err = %v, want Git exit status 128", err)
	}
}
