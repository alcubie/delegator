// Ticket dependencies are explicit database edges: (5, 3) means ticket 5
// waits for ticket 3 to be done. Eligibility checks skip blocked tickets
// without changing their queue positions.

package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// ErrSelfDependency shows that a ticket was told to depend on itself.
var ErrSelfDependency = errors.New("a ticket cannot depend on itself")

// ErrDependencyRing means a dependency would create a cycle and prevent its
// tickets from starting.
var ErrDependencyRing = errors.New("the tickets would depend on one another")

// ErrNoDependency shows that the two tickets have no link to take away.
var ErrNoDependency = errors.New("the ticket does not depend on that one")

// ErrNotQueued means dependencies were changed on a ticket outside the queue.
// Dependencies can only block queued work.
var ErrNotQueued = errors.New("only a queued ticket can change what it depends on")

// AddDependencies makes id depend on each ticket in dependsOn. Existing links
// are unchanged. Only Done satisfies a dependency; a cancelled prerequisite
// blocks work until its link is removed.
func (s *Store) AddDependencies(id int64, dependsOn ...int64) error {
	return s.changeDependencies(id, func(tx *sql.Tx) error {
		return addDependencies(tx, id, dependsOn)
	})
}

// RemoveDependencies takes away the link that makes id depend on each id of
// dependsOn, and gives ErrNoDependency when one of them has no such link.
func (s *Store) RemoveDependencies(id int64, dependsOn ...int64) error {
	return s.changeDependencies(id, func(tx *sql.Tx) error {
		for _, on := range dependsOn {
			result, err := tx.Exec(
				"DELETE FROM ticket_deps WHERE ticket_id = ? AND depends_on = ?", id, on)
			if err != nil {
				return err
			}
			gone, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if gone == 0 {
				return fmt.Errorf("%w: ticket %d does not depend on ticket %d",
					ErrNoDependency, id, on)
			}
		}
		return nil
	})
}

// changeDependencies updates a queued ticket's dependencies atomically. If
// any requested change fails, all changes roll back.
func (s *Store) changeDependencies(id int64, change func(*sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	t, err := ticket(tx, id)
	if err != nil {
		return err
	}
	if t.Status != Queued {
		return fmt.Errorf("%w: ticket %d is %s", ErrNotQueued, id, t.Status)
	}

	if err := change(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// makesRing reports whether adding id -> dependsOn would create a cycle by
// checking whether dependsOn already reaches id.
func makesRing(q querier, id, dependsOn int64) (bool, error) {
	var found int
	err := q.QueryRow(`
		WITH RECURSIVE depends(id) AS (
		  SELECT ?
		  UNION
		  SELECT ticket_deps.depends_on FROM ticket_deps
		  JOIN depends ON depends.id = ticket_deps.ticket_id
		)
		SELECT COUNT(*) FROM depends WHERE id = ?`, dependsOn, id).Scan(&found)
	return found > 0, err
}

// addDependencies inserts dependency edges in the caller's transaction. It
// rejects missing tickets with ErrNoTicket and checks for cycles after each
// insertion, including links added by this call. Duplicate edges are ignored.
func addDependencies(tx *sql.Tx, id int64, dependsOn []int64) error {
	for _, on := range dependsOn {
		if on == id {
			return fmt.Errorf("%w: ticket %d", ErrSelfDependency, id)
		}
		// Check explicitly so ErrNoTicket identifies the missing ID;
		// a foreign-key error would not.
		if err := ticketExists(tx, on); err != nil {
			return err
		}
		ring, err := makesRing(tx, id, on)
		if err != nil {
			return err
		}
		if ring {
			return fmt.Errorf("%w: ticket %d already depends on ticket %d",
				ErrDependencyRing, on, id)
		}
		// ON CONFLICT DO NOTHING: one command can name the same id twice.
		if _, err := tx.Exec(`
			INSERT INTO ticket_deps (ticket_id, depends_on) VALUES (?, ?)
			ON CONFLICT DO NOTHING`, id, on); err != nil {
			return err
		}
	}
	return nil
}

// Dependencies returns all prerequisite IDs in ascending order, including
// completed ones, for dg show.
func (s *Store) Dependencies(id int64) ([]int64, error) {
	rows, err := s.db.Query(
		"SELECT depends_on FROM ticket_deps WHERE ticket_id = ? ORDER BY depends_on", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var on int64
		if err := rows.Scan(&on); err != nil {
			return nil, err
		}
		ids = append(ids, on)
	}
	return ids, rows.Err()
}

// Dependents returns all IDs that depend on id, in ascending order,
// regardless of status.
func (s *Store) Dependents(id int64) ([]int64, error) {
	rows, err := s.db.Query(
		"SELECT ticket_id FROM ticket_deps WHERE depends_on = ? ORDER BY ticket_id", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var dependent int64
		if err := rows.Scan(&dependent); err != nil {
			return nil, err
		}
		ids = append(ids, dependent)
	}
	return ids, rows.Err()
}

// inboxDependencies loads all prerequisite and reverse links in one query.
// Only DependsOn omits completed prerequisites.
func inboxDependencies(q querier) (map[int64][]int64, map[int64][]int64, map[int64][]int64, error) {
	rows, err := q.Query(`
  SELECT ticket_deps.ticket_id, ticket_deps.depends_on, tickets.status
  FROM ticket_deps
  JOIN tickets ON tickets.id = ticket_deps.depends_on
  ORDER BY ticket_deps.ticket_id, ticket_deps.depends_on`)
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()
	unmet := make(map[int64][]int64)
	all := make(map[int64][]int64)
	blocks := make(map[int64][]int64)
	for rows.Next() {
		var id, on int64
		var status TicketStatus
		if err := rows.Scan(&id, &on, &status); err != nil {
			return nil, nil, nil, err
		}
		all[id] = append(all[id], on)
		blocks[on] = append(blocks[on], id)
		if status != Done {
			unmet[id] = append(unmet[id], on)
		}
	}
	return unmet, all, blocks, rows.Err()
}
