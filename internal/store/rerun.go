package store

import (
	"database/sql"
	"fmt"
	"time"
)

// Rerun marks a ready ticket running and records a new run for agentID,
// using pid as its supervisor PID. It returns ErrNoTicket for a missing
// ticket and ErrInvalidTicketStateChange for any status other than Ready.
//
// There is no supervisor here: dg ticket chat already holds the session, and
// pid is its process, not a child this call starts. The transition and run
// record share one transaction so a running ticket always has an agent and a
// PID.
func (s *Store) Rerun(id, agentID int64, pid int) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	from, err := statusOf(tx, id)
	if err != nil {
		return 0, err
	}
	if from != Ready {
		return 0, fmt.Errorf("%w: the ticket is %s, and only a ready ticket reruns",
			ErrInvalidTicketStateChange, from)
	}
	started := time.Now()
	if err := changeStatus(tx, id, Running, started); err != nil {
		return 0, err
	}
	runID, err := startRun(tx, id, agentID, sql.Null[int64]{}, pid, started)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return runID, nil
}
