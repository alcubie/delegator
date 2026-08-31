package run

import (
	"github.com/alcubie/delegator/internal/adapters"
	"github.com/alcubie/delegator/internal/store"
)

// Start runs the ticket with the given id: it creates the worktree and its
// branch, claims the ticket for this run, then runs the agent in the worktree
// and waits for it to exit.
//
// The worktree is created first. If git refuses, the ticket stays queued where
// you can see it, rather than sitting in running with nowhere to work.
func Start(dataDir string, id int64, agent adapters.Adapter) error {
	s, err := store.Open(dataDir)
	if err != nil {
		return err
	}
	defer s.Close()

	ticket, err := s.Ticket(id)
	if err != nil {
		return err
	}

	worktree, err := Worktree(dataDir, ticket)
	if err != nil {
		return err
	}
	if err := s.Claim(id, branch(id, ticket.Title)); err != nil {
		return err
	}

	return agent.Launch(adapters.RunSpec{Worktree: worktree}).Run()
}
