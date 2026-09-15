// The restart of a failed ticket, which dg restart makes.

package store

import (
	"fmt"
	"os"
	"time"
)

// Restart marks a failed ticket running and writes the row for the new run.
// The ticket keeps its branch and its session, and nothing removes its
// worktree, so the run that follows continues the work of the run that failed.
//
// Every status but failed gives ErrInvalidTicketStateChange, and an id that
// holds no ticket gives ErrNoTicket. The table of the state machine allows
// ready to queued, which is dg revise giving work back with more instructions,
// so the status is read here rather than left to that table: the table says
// what a change is, and not which command makes it.
//
// The transition and the row of the run are one transaction, so a ticket in
// running always has the process id of the supervisor that restarted it.
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
	result, err := tx.Exec(
		"INSERT INTO runs (ticket_id, pid, started_at) VALUES (?, ?, ?)",
		id, os.Getpid(), rfc3339(started),
	)
	if err != nil {
		return 0, err
	}
	runID, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return runID, nil
}
