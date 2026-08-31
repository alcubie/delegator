// Package run holds the functions needed for initiating a run of a ticket6
package run

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/gosimple/slug"

	"github.com/alcubie/delegator/internal/project"
)

// branchPrefix is the prefix attached to all branch names
const branchPrefix = "delegator"

// maxSlug is the max length of the branch slug that can be returned
const maxSlug = 40

func init() { slug.MaxLength = maxSlug }

// branch returns the branch name for a ticket given its id and title.
// The slugified title is limited to at most 40 characters and will be only ASCII
// characters.
func branch(id int64, title string) string {
	generated := slug.Make(title)
	if generated == "" {
		return fmt.Sprintf("%s/%d", branchPrefix, id)
	}

	return fmt.Sprintf("%s/%d-%s", branchPrefix, id, generated)
}

// Worktree makes the worktree for one run at <dataDir>/worktrees/<id>, checked
// out on a new branch that starts at defaultBranch. It returns the path of the
// worktree.
func Worktree(dataDir, projectPath, defaultBranch string, id int64, title string) (string, error) {
	path := filepath.Join(dataDir, "worktrees", strconv.FormatInt(id, 10))
	if err := project.AddWorktree(projectPath, path, branch(id, title), defaultBranch); err != nil {
		return "", err
	}
	return path, nil
}
