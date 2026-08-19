package project

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrGitNotOnPath shows that git is not installed on the PATH.
var ErrGitNotOnPath = errors.New("git is not on the PATH")

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
// Section 7 gives the value.
const keyHashLength = 6

// Key gives the name of the directory that holds the data of one project. It is
// the name of the root directory, and then 6 characters from the SHA-256 hash of
// the full path. The name alone is not unique, because many repositories have
// the name "backend". Section 7 gives the reason, and project.toml holds the
// full path, so the key is reversible.
func Key(root string) string {
	sum := sha256.Sum256([]byte(root))
	return filepath.Base(root) + "-" + hex.EncodeToString(sum[:])[:keyHashLength]
}

// dirPerm gives the permission of each directory that delegator makes. Section
// 11 says that a ticket can contain private data, so only its person can read
// it. A directory that gives no permission to a group stops a read of the names
// of the files below it.
const dirPerm = 0o700

// projectDirs are the directories that each project has. The tickets hold the
// work, the worktrees hold the copy of the repository for each ticket, and the
// runs hold the log of the agent.
var projectDirs = []string{"tickets", "worktrees", "runs"}

// Create makes the directory of one project below dataDir, and the directories
// below it. It gives the path of the directory of the project. Create makes a
// directory that is already present again, and this causes no error.
func Create(dataDir, root string) (string, error) {
	dir := filepath.Join(dataDir, "projects", Key(root))
	for _, name := range projectDirs {
		if err := os.MkdirAll(filepath.Join(dir, name), dirPerm); err != nil {
			return "", err
		}
	}
	return dir, nil
}
