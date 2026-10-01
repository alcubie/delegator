package store

import (
	"fmt"
	"time"
)

// Restart marks a failed ticket running and creates a new run for agentID,
// preserving its branch, session, and worktree. It returns ErrNoTicket for a
// missing ticket and ErrInvalidTicketStateChange for any status other than
// Failed.
//
// The transition and run record share one transaction so a running ticket
// always has an agent and supervisor PID.
func (s *Store) Restart(id, agentID int64) (int64, error) {
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
	runID, err := startRun(tx, id, agentID, started)
	if err != nil {
		return 0, err
	}
	// Carry the prior selection forward before attempting ACP setup, so a
	// failed load does not erase it on a subsequent restart.
	if _, err := tx.Exec(`UPDATE runs SET model_id = (
SELECT model_id FROM runs WHERE ticket_id = ? AND id < ? ORDER BY id DESC LIMIT 1
) WHERE id = ? AND EXISTS (
SELECT 1 FROM tickets WHERE id = ? AND COALESCE(session, '') <> ''
) AND agent_id = (
SELECT agent_id FROM runs WHERE ticket_id = ? AND id < ? ORDER BY id DESC LIMIT 1
)`, id, runID, runID, id, id, runID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return runID, nil
}
