package run

import (
	"github.com/alcubie/delegator/internal/store"
)

// Start runs the ticket with the given id: it creates the worktree and its
// branch, then claims the ticket for this run.
//
// The worktree is created first. If git refuses, the ticket stays queued where
// you can see it, rather than sitting in running with nowhere to work.
func Start(dataDir string, id int64) error {
	s, err := store.Open(dataDir)
	if err != nil {
		return err
	}
	defer s.Close()

	ticket, err := s.Ticket(id)
	if err != nil {
		return err
	}

	if _, err := Worktree(dataDir, ticket.Project.Path, ticket.Project.DefaultBranch, id, ticket.Title); err != nil {
		return err
	}
	return s.Claim(id, branch(id, ticket.Title))
}
