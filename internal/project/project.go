// Package project reads a git repository. It gives the root of the repository,
// the default branch and the first commit.
//
// Delegator accepts a ticket only if the directory is below a repository, so
// this package is the boundary at git. It starts the git command and reads the
// output, and no other package of delegator does.
package project

import (
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
)

// ErrGitNotOnPath shows that git is not installed on the PATH.
var ErrGitNotOnPath = errors.New("git is not on the PATH")

// ErrNoCommit shows that a repository has no commit, so it has no first commit
// to give it an identity.
var ErrNoCommit = errors.New("the repository has no commit")

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

// gitCommand returns the command for one git call in root, with each variable
// of gitEnv taken out of the environment that it gets.
func gitCommand(root string, args ...string) *exec.Cmd {
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

// Root returns the top-level git directory for the argument.
// It returns an error if the path is not in a git repository.
func Root(path string) (string, error) {
	gitDir, err := gitCommand(path, "rev-parse", "--show-toplevel").Output()
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

	return strings.TrimSuffix(string(gitDir), "\n"), nil
}

// gitOutput runs one git command in root and returns its output with no final
// newline. An error means that git said no, and each caller decides what that
// answer means.
func gitOutput(root string, args ...string) (string, error) {
	out, err := gitCommand(root, args...).Output()
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
