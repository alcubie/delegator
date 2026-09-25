package store

import (
	"fmt"
	"time"
)

// Restart marks a failed ticket running and creates a new run, preserving its
// branch, session, and worktree. It returns ErrNoTicket for a missing ticket
// and ErrInvalidTicketStateChange for any status other than Failed.
//
// The transition and run record share one transaction so a running ticket
// always has a supervisor PID.
func (s *Store) Restart(id int64) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	from, err := statusOf(tx, id)
	if err != nil {
		return 0, err
	}
	if from != Failed {
		return 0, fmt.Errorf("%w: the ticket is %s, and only a failed ticket restarts",
			ErrInvalidTicketStateChange, from)
	}
	started := time.Now()
	if err := changeStatus(tx, id, Running, started); err != nil {
		return 0, err
	}
	runID, err := startRun(tx, id, started)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return runID, nil
}
