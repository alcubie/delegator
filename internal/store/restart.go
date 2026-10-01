package store

import (
	"database/sql"
	"fmt"
	"time"
)

// Restart marks a failed ticket running and creates a new run for agentID,
// preserving its branch, session, and worktree. It returns ErrNoTicket for a
// missing ticket and ErrInvalidTicketStateChange for any status other than
// Failed.
//
// The transition and run record share one transaction so a running ticket
// always has an agent and supervisor PID. The caller supplies the prior
// session's modelID, or NULL when there is no recorded model to inherit.
func (s *Store) Restart(id, agentID int64, modelID sql.Null[int64]) (int64, error) {
	return s.restart(id, agentID, modelID, "")
}

// RestartWithModel records the requested model on the new run before agent
// setup. The agent's confirmed selection may replace it during setup.
func (s *Store) RestartWithModel(id, agentID int64, model string) (int64, error) {
	return s.restart(id, agentID, sql.Null[int64]{}, model)
}

func (s *Store) restart(id, agentID int64, modelID sql.Null[int64], model string) (int64, error) {
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
	if model != "" {
		modelID, err = registerModel(tx, agentID, model)
		if err != nil {
			return 0, err
		}
	}
	started := time.Now()
	if err := changeStatus(tx, id, Running, started); err != nil {
		return 0, err
	}
	runID, err := startRun(tx, id, agentID, modelID, started)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return runID, nil
}
