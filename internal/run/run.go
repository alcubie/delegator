// Package run manages ticket worktrees, supervisors, and agent runs.
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

const branchPrefix = "delegator"

// ProjectCacheEnvironment names the project cache in an agent process. It is
// deliberately neutral: an agent can choose which tools, if any, use it.
const ProjectCacheEnvironment = "DELEGATOR_PROJECT_CACHE_DIR"

// maxSlug limits the title portion of a ticket branch name.
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

// WorktreePath returns the ticket worktree path. Using the ticket ID keeps it
// stable when the project moves.
func WorktreePath(dataDir string, id int64) string {
	return filepath.Join(dataDir, "worktrees", strconv.FormatInt(id, 10))
}

// ProjectCachePath returns the stable cache directory of one project. The
// cache subtree is disposable data, unlike tickets, logs, and the database;
// it is outside worktrees so accepting a ticket does not remove it.
func ProjectCachePath(dataDir string, projectID int64) string {
	return filepath.Join(dataDir, "cache", "projects", strconv.FormatInt(projectID, 10))
}

// ProjectCache makes the private cache directory of one project and returns
// its path. MkdirAll also makes each cache parent private when it is new.
func ProjectCache(dataDir string, projectID int64) (string, error) {
	path := ProjectCachePath(dataDir, projectID)
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", err
	}
	return path, nil
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

// AcceptanceWorktree describes what acceptance found at a ticket's worktree
// path. Only a registered worktree is safe to pass to Git for removal.
type AcceptanceWorktree int

const (
	WorktreeAbsent AcceptanceWorktree = iota
	WorktreeRegistered
	WorktreeUnregistered
)

// ValidateAcceptanceWorktree determines whether acceptance may proceed and
// whether cleanup is safe. A leftover directory without a .git entry is
// preserved because its contents cannot be classified reliably.
func ValidateAcceptanceWorktree(path string, force bool) (AcceptanceWorktree, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return WorktreeAbsent, nil
	}
	if err != nil {
		return WorktreeAbsent, err
	}
	if !info.IsDir() {
		return WorktreeAbsent, fmt.Errorf("worktree path %s is not a directory", path)
	}

	if _, err := os.Lstat(filepath.Join(path, ".git")); os.IsNotExist(err) {
		return WorktreeUnregistered, nil
	} else if err != nil {
		return WorktreeAbsent, err
	}
	if !force {
		if err := project.RequireCleanWorktree(path); err != nil {
			return WorktreeAbsent, err
		}
	}
	return WorktreeRegistered, nil
}

// RemoveWorktree removes the worktree of one run. A worktree that is not there
// is not an error: a command that failed after the removal can then run again.
// force removes a worktree holding changes that are not committed.
func RemoveWorktree(dataDir string, ticket store.Ticket, force bool) error {
	path := WorktreePath(dataDir, ticket.ID)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	return project.RemoveWorktree(ticket.Project.Path, path, force)
}
