package project

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/alcubie/delegator/internal/atomicfile"
)

// ErrGitNotOnPath shows that git is not installed on the PATH.
var ErrGitNotOnPath = errors.New("git is not on the PATH")

// ErrNoCommit shows that a repository has no commit, so it has no first commit
// to give it an identity.
var ErrNoCommit = errors.New("the repository has no commit")

// ErrNotARepository shows that the path is not under git version control.
var ErrNotARepository = errors.New("the directory is not under git version control")

// Root returns the top-level git directory for the argument.
// It returns an error if the path is not in a git repository.
func Root(path string) (string, error) {
	cmd := exec.Command("git", "-C", path, "rev-parse", "--show-toplevel")

	gitDir, err := cmd.Output()
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

// keyHashLength is the number of characters that the key takes from the hash.
// Six characters give 16 million values, which is sufficient to separate the
// repositories of one person, and the name of the directory stays short enough
// to read and to type.
const keyHashLength = 6

// Key gives the name of the directory that holds the data of one project. It is
// the name of the root directory, and then 6 characters from the SHA-256 hash of
// the full path. The name alone is not unique, because many repositories have
// the name "backend". The file project.toml holds the full path, so the key is
// reversible.
func Key(root string) string {
	sum := sha256.Sum256([]byte(root))
	return filepath.Base(root) + "-" + hex.EncodeToString(sum[:])[:keyHashLength]
}

// dirPerm gives the permission of each directory that delegator makes. A ticket
// can contain private data, so only its person can read it. A directory that
// gives no permission to a group stops a read of the names of the files below
// it.
const dirPerm = 0o700

// projectDirs are the directories that each project has. The tickets hold the
// work, the worktrees hold the copy of the repository for each ticket, and the
// runs hold the log of the agent.
var projectDirs = []string{"tickets", "worktrees", "runs"}

// Project holds the fields of project.toml. The path makes the key reversible,
// and the default branch is the start of the branch of each run: each run gets
// one worktree and one branch, and that branch comes from the default branch.
type Project struct {
	Path          string `toml:"path"`
	DefaultBranch string `toml:"default_branch"`
}

// Create makes the directory of one project below dataDir, and the directories
// below it, and writes project.toml if it does not exist. If project.toml does
// exist it is left unmodified. It gives the path of the directory of the project.
// Create makes a directory that is already present again, and this causes no
// error.
//
// Create takes the default branch, and does not read it from the repository,
// because no one answer from git is correct for each repository. DefaultBranch
// asks git and gives the best answer that it has, and the caller decides
// whether to use it.
func Create(dataDir, root, defaultBranch string) (string, error) {
	dir := Dir(dataDir, root)
	for _, name := range projectDirs {
		if err := os.MkdirAll(filepath.Join(dir, name), dirPerm); err != nil {
			return "", err
		}
	}

	path := tomlPath(dir)
	exists, err := pathExists(path)
	if err != nil {
		return "", err
	}
	if exists {
		return dir, nil
	}

	data, err := toml.Marshal(Project{Path: root, DefaultBranch: defaultBranch})
	if err != nil {
		return "", err
	}

	if err := atomicfile.Write(path, data, atomicfile.Perm); err != nil {
		return "", err
	}
	return dir, nil
}

// Dir returns the directory below dataDir that holds the data of the project.
// The root is the path of the git repository and Key creates its unique name.
func Dir(dataDir string, root string) string {
	return filepath.Join(dataDir, "projects", Key(root))
}

func tomlPath(projectDir string) string {
	return filepath.Join(projectDir, "project.toml")
}

func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Config returns the configuration of the project located in project.toml
func Config(projectDir string) (Project, error) {
	var config Project
	path := tomlPath(projectDir)
	data, err := os.ReadFile(path)
	if err != nil {
		return config, err
	}

	_, err = toml.Decode(string(data), &config)
	if err != nil {
		return config, err
	}

	return config, nil
}

// gitOutput runs one git command in root and gives its output with no final
// newline. An error means that git said no, and each caller decides what that
// answer means.
func gitOutput(root string, args ...string) (string, error) {
	all := append([]string{"-C", root}, args...)
	out, err := exec.Command("git", all...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// trunkNames are the two names that a repository with no remote is most likely
// to give its main line of work. They come before the branch of HEAD, because
// the person can be on a branch of a feature at this moment.
var trunkNames = []string{"main", "master"}

// DefaultBranch gives the branch that each run of a ticket starts from. Section
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

// FirstCommit gives the hash of the commit that has no parent. The hash of a
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
