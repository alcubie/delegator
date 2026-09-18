// Package project reads a git repository. It gives the root of the repository,
// the default branch and the first commit.
//
// Delegator accepts a ticket only if the directory is below a repository, so
// this package is the boundary at git. It starts the git command and reads the
// output, and no other package of delegator does.
package project

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// ErrGitNotOnPath shows that git is not installed on the PATH.
var ErrGitNotOnPath = errors.New("git is not on the PATH")

// ErrNoCommit shows that a repository has no commit, so it has no first commit
// to give it an identity.
var ErrNoCommit = errors.New("the repository has no commit")

// ErrUnknownCommit shows that a revision does not name a commit in the
// repository.
var ErrUnknownCommit = errors.New("git does not know the commit")

// ErrCommitNotOnBranch shows that a branch does not contain the commit.
var ErrCommitNotOnBranch = errors.New("the ticket branch does not hold the commit")

// ErrBranchNotMerged shows that HEAD does not contain the ticket branch or an
// equivalent squash of all its changes.
var ErrBranchNotMerged = errors.New("the ticket branch is not merged into HEAD")

// ErrNotARepository shows that the path is not under git version control.
var ErrNotARepository = errors.New("the directory is not under git version control")

// gitEnv are the variables that pin a git command to one repository. Delegator
// names the repository with -C, so a value that another git left in the
// environment must not reach the command. A hook of git sets GIT_DIR and
// GIT_INDEX_FILE to paths that are relative to the root of the repository, and
// a command of delegator that runs in a different directory then reads a file
// that is not there.
var gitEnv = []string{
	"GIT_ALTERNATE_OBJECT_DIRECTORIES",
	"GIT_COMMON_DIR",
	"GIT_DIR",
	"GIT_INDEX_FILE",
	"GIT_OBJECT_DIRECTORY",
	"GIT_PREFIX",
	"GIT_WORK_TREE",
}

// Command returns the command for one git call in root, with each variable of
// gitEnv taken out of the environment that it gets.
//
// It is exported for the fixtures of a test, which build a repository to run
// delegator against and meet the same environment. No other package of
// delegator starts git at run time.
func Command(root string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = withoutGitEnv(os.Environ())
	return cmd
}

// withoutGitEnv returns env with each variable of gitEnv removed.
func withoutGitEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, pair := range env {
		if name, _, ok := strings.Cut(pair, "="); ok && slices.Contains(gitEnv, name) {
			continue
		}
		out = append(out, pair)
	}
	return out
}

// Root returns the primary checkout of the repository that holds the argument.
// It returns an error if the path is not in a git repository.
func Root(path string) (string, error) {
	gitDir, err := Command(path, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", ErrGitNotOnPath
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", ErrNotARepository
		}
		return "", err
	}

	return filepath.Dir(strings.TrimSuffix(string(gitDir), "\n")), nil
}

// The text of an exec.ExitError is the exit status alone, so a failure would
// reach the person as a number and the sentence git wrote would be lost. The
// error carries the stderr of git, and commandOutput puts it in front of the
// status. The exec.ExitError stays underneath, because callers match on the
// type.
func commandOutput(cmd *exec.Cmd) ([]byte, error) {
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if stderr := strings.TrimRight(string(exitErr.Stderr), "\n"); stderr != "" {
				return nil, fmt.Errorf("%s: %w", stderr, err)
			}
		}
		return nil, err
	}
	return out, nil
}

// gitOutput runs one git command in root and returns its output with no final
// newline. An error means that git said no, and each caller decides what that
// answer means.
func gitOutput(root string, args ...string) (string, error) {
	out, err := commandOutput(Command(root, args...))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// trunkNames are the two names that a repository with no remote is most likely
// to give its main line of work. They come before the branch of HEAD, because
// the person can be on a branch of a feature at this moment.
var trunkNames = []string{"main", "master"}

// DefaultBranch returns the branch that each run of a ticket starts from. Section
// 6.5 gives the reason that the value matters.
//
// No one answer from git is correct for each repository, so this asks four
// questions and takes the first answer:
//
//  1. refs/remotes/origin/HEAD. The remote says which branch it gives by
//     default. This is the best answer, but a repository with no remote, and a
//     clone that came from a fetch with no head, do not have it.
//  2. A local branch with the name main.
//  3. A local branch with the name master.
//  4. The branch that HEAD points at. This answers for a repository that has
//     no remote and a different name for its main line, and it also answers
//     for a new repository that has no commit.
func DefaultBranch(root string) (string, error) {
	if out, err := gitOutput(root, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		// The short form is origin/develop, and the name of the branch on the
		// remote is the part that comes after the name of the remote.
		return strings.TrimPrefix(out, "origin/"), nil
	}

	for _, name := range trunkNames {
		if _, err := gitOutput(root, "rev-parse", "--verify", "--quiet", "refs/heads/"+name); err == nil {
			return name, nil
		}
	}

	out, err := gitOutput(root, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", ErrGitNotOnPath
		}
		return "", err
	}
	return out, nil
}

// FirstCommit returns the hash of the commit that has no parent. The hash of a
// repository does not change when the person moves it, so it is the one value
// that finds a project again after a move.
//
// A copy of a repository has the same first commit as its source, so this value
// shows that two repositories can be the same. It does not prove that they are
// the same, and the person makes that decision.
func FirstCommit(root string) (string, error) {
	out, err := gitOutput(root, "rev-list", "--max-parents=0", "HEAD")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", ErrGitNotOnPath
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", ErrNoCommit
		}
		return "", err
	}

	// A merge of two histories that had no relation gives more than one commit
	// with no parent. rev-list gives the newest commit first, so the last line
	// is the first commit of the repository.
	lines := strings.Split(out, "\n")
	return lines[len(lines)-1], nil
}

// AddWorktree makes a worktree at path, checked out on a new branch that starts
// at from. Git makes the branch together with the worktree, so the caller does
// not make it first.
func AddWorktree(root, path, branch, from string) error {
	_, err := gitOutput(root, "worktree", "add", "-b", branch, path, from)
	return err
}

// Commit returns the short hash of one commit and the subject of its message.
// An error means git does not know the hash.
func Commit(root, hash string) (short, subject string, err error) {
	out, err := gitOutput(root, "show", "-s", "--format=%h%n%s", hash)
	if err != nil {
		return "", "", err
	}
	short, subject, _ = strings.Cut(out, "\n")
	return short, subject, nil
}

// resolveCommit returns the full hash of the commit named by revision. An
// error means the repository does not hold a commit with that name.
func resolveCommit(root, revision string) (string, error) {
	full, err := gitOutput(root, "rev-parse", "--verify", "--quiet", "--end-of-options", revision+"^{commit}")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", ErrGitNotOnPath
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("%w: %s", ErrUnknownCommit, revision)
		}
		return "", err
	}
	return full, nil
}

// CommitOnBranch returns the full hash of the commit named by revision when
// branch contains it. It returns ErrUnknownCommit when revision names no
// commit, and ErrCommitNotOnBranch when the commit is outside branch.
func CommitOnBranch(root, revision, branch string) (string, error) {
	full, err := resolveCommit(root, revision)
	if err != nil {
		return "", err
	}
	_, err = gitOutput(root, "merge-base", "--is-ancestor", full, "refs/heads/"+branch)
	if err == nil {
		return full, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return "", fmt.Errorf("%w: %s is not on %s", ErrCommitNotOnBranch, full, branch)
	}
	return "", err
}

// RequireBranchMerged returns nil when HEAD contains branch, either through an
// ordinary merge or through one commit whose stable patch is the complete
// change of branch. The second form recognizes a squash merge without
// accepting one part of the branch or a commit that bundles other work with
// it.
//
// The caller gives the primary checkout of the project. HEAD is deliberately
// resolved there rather than in the directory from which a command happened
// to run.
func RequireBranchMerged(root, branch string) error {
	ref := "refs/heads/" + branch
	merged, err := isAncestor(root, ref, "HEAD")
	if err != nil {
		return err
	}
	if merged {
		return nil
	}

	equivalent, err := hasEquivalentSquash(root, ref)
	if err != nil {
		return err
	}
	if equivalent {
		return nil
	}
	return fmt.Errorf("%w: %s; use dg accept --force to accept it anyway", ErrBranchNotMerged, branch)
}

// isAncestor gives exit status 1 its documented meaning and preserves every
// other failure from Git.
func isAncestor(root, ancestor, descendant string) (bool, error) {
	_, err := gitOutput(root, "merge-base", "--is-ancestor", ancestor, descendant)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return false, ErrGitNotOnPath
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}

// hasEquivalentSquash compares one patch for the complete branch change with
// one patch for each commit added to HEAD since the histories separated. A
// match is therefore a squash of all the work, not only a matching commit from
// a branch that holds additional changes.
func hasEquivalentSquash(root, branch string) (bool, error) {
	base, err := gitOutput(root, "merge-base", branch, "HEAD")
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}
	want, err := stablePatchID(root, "diff", base, branch)
	if err != nil || want == "" {
		return false, err
	}

	commits, err := gitOutput(root, "rev-list", "--no-merges", base+"..HEAD")
	if err != nil {
		return false, err
	}
	if commits == "" {
		return false, nil
	}
	for _, commit := range strings.Split(commits, "\n") {
		got, err := stablePatchID(root, "show", commit)
		if err != nil {
			return false, err
		}
		if got == want {
			return true, nil
		}
	}
	return false, nil
}

// stablePatchID returns Git's stable identity for a tree-to-tree diff or one
// commit. Rename detection and external diff drivers are disabled so the same
// repository content gives the same patch in both forms.
func stablePatchID(root, form string, revisions ...string) (string, error) {
	args := []string{form, "--no-ext-diff", "--no-textconv", "--binary", "--full-index", "--no-renames"}
	if form == "show" {
		args = append(args, "--root", "--format=")
	}
	patch, err := commandOutput(Command(root, append(args, revisions...)...))
	if err != nil {
		return "", err
	}

	cmd := Command(root, "patch-id", "--stable")
	cmd.Stdin = bytes.NewReader(patch)
	out, err := commandOutput(cmd)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", nil
	}
	return fields[0], nil
}

// RemoveWorktree removes the worktree at path and the record git keeps of it.
// Git refuses a worktree holding changes that are not committed, which is why
// every run must end with a commit. force takes the worktree anyway, and the
// changes that are not committed go with it.
func RemoveWorktree(root, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	_, err := gitOutput(root, append(args, path)...)
	return err
}
