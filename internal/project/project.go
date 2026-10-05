// Package project provides repository metadata and worktree operations. It
// centralizes Git subprocess calls for delegator.
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

// ErrWorktreeDirty shows that a worktree has staged, unstaged, or untracked
// changes which acceptance would discard.
var ErrWorktreeDirty = errors.New("the worktree has uncommitted changes")

// ErrNotARepository shows that the path is not under git version control.
var ErrNotARepository = errors.New("the directory is not under git version control")

// gitEnv lists variables that override repository selection. Strip them
// before using git -C: hooks can set GIT_DIR and GIT_INDEX_FILE to relative
// paths that become invalid in another directory.
var gitEnv = []string{
	"GIT_ALTERNATE_OBJECT_DIRECTORIES",
	"GIT_COMMON_DIR",
	"GIT_DIR",
	"GIT_INDEX_FILE",
	"GIT_OBJECT_DIRECTORY",
	"GIT_PREFIX",
	"GIT_WORK_TREE",
}

// Command builds a Git command in root with gitEnv variables removed. It is
// exported so test fixtures use the same environment handling as production
// calls.
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

// commandOutput includes Git stderr in errors while preserving exec.ExitError
// for callers that inspect its type.
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

// gitOutput runs Git in root and trims surrounding whitespace from its
// output. Callers interpret command failures.
func gitOutput(root string, args ...string) (string, error) {
	out, err := commandOutput(Command(root, args...))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// trunkNames lists common default branches for repositories without a remote.
// Prefer these over HEAD, which may point to a feature branch.
var trunkNames = []string{"main", "master"}

// DefaultBranch returns the base branch for ticket runs. It tries
// origin/HEAD, local main, local master, then the branch named by HEAD. The
// last fallback also works before the first commit.
func DefaultBranch(root string) (string, error) {
	if out, err := gitOutput(root, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		// Strip the remote name from the short ref, for example
		// origin/develop.
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

// FirstCommit returns a root commit hash used to recognize projects after a
// directory move. Clones share this hash, so a match suggests a project
// identity but still requires user confirmation.
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

	// Unrelated histories can have multiple root commits. Take the last
	// root in rev-list order.
	lines := strings.Split(out, "\n")
	return lines[len(lines)-1], nil
}

// AddWorktree creates a worktree at path and a new branch starting at from.
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

// RequireBranchMerged checks that HEAD contains branch, either by ancestry or
// by a single commit with an equivalent patch for the entire branch. Partial
// squashes and squashes containing unrelated changes do not qualify.
//
// The caller must pass the primary checkout so HEAD refers to the project
// checkout, not the ticket worktree.
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

// RequireCleanWorktree checks tracked changes and all non-ignored untracked
// files. Git reports inspection failures separately from a dirty result.
func RequireCleanWorktree(path string) error {
	status, err := gitOutput(path, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return err
	}
	if status != "" {
		return fmt.Errorf("%w; use dg accept --force to accept it anyway", ErrWorktreeDirty)
	}
	return nil
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

// hasEquivalentSquash compares the full branch diff with each commit added to
// HEAD since the merge base. A match must cover all branch changes.
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

// RemoveWorktree removes the worktree and its Git registration. Unless force
// is true, Git refuses to remove uncommitted changes.
func RemoveWorktree(root, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	_, err := gitOutput(root, append(args, path)...)
	return err
}
