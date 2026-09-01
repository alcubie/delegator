// Package run holds the functions needed for initiating a run of a ticket
package run

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/gosimple/slug"

	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/store"
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

// WorktreePath returns the directory a run works in. Section 7 gives it the id
// of the ticket and nothing else, so no name on the disk holds a project.
func WorktreePath(dataDir string, id int64) string {
	return filepath.Join(dataDir, "worktrees", strconv.FormatInt(id, 10))
}

// Worktree creates the worktree for one run at <dataDir>/worktrees/<id>, on a
// new branch cut from the project's default branch, and returns its path.
//
// An existing worktree is returned unchanged: a restart works in the one the
// earlier run created.
func Worktree(dataDir string, ticket store.Ticket) (string, error) {
	path := WorktreePath(dataDir, ticket.ID)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	name := branch(ticket.ID, ticket.Title)
	if err := project.AddWorktree(ticket.Project.Path, path, name, ticket.Project.DefaultBranch); err != nil {
		return "", err
	}
	return path, nil
}
