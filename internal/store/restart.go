// The return of a failed ticket to the queue, which dg restart makes.

package store

import (
	"fmt"
	"time"
)

// Restart puts a failed ticket at the end of the queue, so that the next
// supervisor claims it again. The ticket keeps its branch and its session, and
// nothing removes its worktree, so the run that follows continues the work of
// the run that failed.
//
// Every status but failed gives ErrInvalidTicketStateChange, and an id that
// holds no ticket gives ErrNoTicket. The table of the state machine allows
// ready to queued, which is dg revise giving work back with more instructions,
// so the status is read here rather than left to that table: the table says
// what a change is, and not which command makes it.
//
// The read and the write are one transaction, so a supervisor that claims the
// ticket at the same moment either finds it failed and takes nothing, or finds
// it queued after this has written.
func (s *Store) Restart(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	from, err := statusOf(tx, id)
	if err != nil {
		return err
	}
	if from != Failed {
		return fmt.Errorf("%w: the ticket is %s, and only a failed ticket restarts",
			ErrInvalidTicketStateChange, from)
	}
	if err := changeStatus(tx, id, Queued, time.Now()); err != nil {
		return err
	}
	return tx.Commit()
}
