// The links that make one ticket depend on another. A link is explicit, so the
// order that the work has to go in is a fact the database holds and not a
// sentence in the prose that only a person can read.
//
// A link is a row of ticket_deps: (5, 3) says that ticket 5 depends on ticket 3.
// One rule comes out of it, and it is in nextWithRoom: the queue passes over a
// ticket that depends on a ticket that is not done. Nothing else about a link
// constrains the queue, so the place a person gives a ticket is still theirs.
//
// AddDependencies and RemoveDependencies are the writes behind dg depend. They
// refuse a link that no work could ever satisfy: a ticket that depends on
// itself, and a ring of tickets that each depend on the next.
//
// Two reads are here as well. unmetDependencies answers, for the whole inbox at
// once, which links still hold a ticket back, Dependencies gives every link of
// one ticket for dg show, and Dependents gives every ticket that waits on one.

package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// ErrSelfDependency shows that a ticket was told to depend on itself.
var ErrSelfDependency = errors.New("a ticket cannot depend on itself")

// ErrDependencyRing shows that a link would make a ring of tickets that each
// depend on the next. No ticket of a ring can ever start, so the link is
// refused rather than written.
var ErrDependencyRing = errors.New("the tickets would depend on one another")

// ErrNoDependency shows that the two tickets have no link to take away.
var ErrNoDependency = errors.New("the ticket does not depend on that one")

// ErrNotQueued shows that a link was asked for on a ticket that has left the
// queue. A link only holds a ticket back in the queue, so a link on a ticket
// that is running, ready or done would change nothing and is refused rather
// than written.
var ErrNotQueued = errors.New("only a queued ticket can change what it depends on")

// AddDependencies records that the ticket id depends on each id of dependsOn.
// The queue passes id over until each of them is done.
//
// A link that is there already is not an error. The person asked for a state,
// and the state is what they asked for, so the second command says the same
// thing as the first.
//
// A link to a ticket that is cancelled is allowed and never becomes satisfied,
// because only done satisfies one. The inbox names the ticket it depends on, so
// the person can see which link to take away.
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

// changeDependencies runs change on the links of a queued ticket, under one
// transaction, so a command that names several ids and has to refuse one of
// them writes none of them.
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

// makesRing reports whether a link from id to dependsOn would make a ring. It
// walks the links that dependsOn has already, over each ticket that one depends
// on in turn, and a ring forms when that walk reaches id.
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

// addDependencies writes a link for each id of dependsOn, saying that the
// ticket id depends on it. An id that names no ticket gives ErrNoTicket, so a
// mistyped id stops the write rather than making a link that nothing can ever
// satisfy.
//
// The same id twice is one row. The primary key of the table is the pair, and
// the caller asked for a state rather than for a count of rows.
//
// Each link is checked against the links that are already written, and the
// ones this call has written, so a command that names two ids cannot make a
// ring out of the pair of them.
func addDependencies(tx *sql.Tx, id int64, dependsOn []int64) error {
	for _, on := range dependsOn {
		if on == id {
			return fmt.Errorf("%w: ticket %d", ErrSelfDependency, id)
		}
		// Checked here so a missing id returns ErrNoTicket naming that id.
		// The INSERT below would instead fail with SQLite error 787,
		// "FOREIGN KEY constraint failed", which names no id.
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

// Dependencies returns the id of each ticket that id depends on, in the order
// of the ids. It gives every link and not only the ones that still hold the
// ticket back, because dg show writes what the person made.
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

// Dependents returns the id of each ticket that depends on id, in the order of
// the ids. It gives every link, whether or not the ticket is ready to run, so
// dg show can name the work that finishing one ticket lets go.
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

// unmetDependencies returns, for each ticket that depends on a ticket that is
// not done, the ids of the tickets it still depends on. A ticket whose links are
// all satisfied is not in the map, and neither is a ticket with no link.
//
// It is one query for the whole inbox rather than one for each row, because the
// inbox reads every open ticket at one time and a query for each row would grow
// with the queue.
func unmetDependencies(q querier) (map[int64][]int64, error) {
	rows, err := q.Query(`
		SELECT ticket_deps.ticket_id, ticket_deps.depends_on
		FROM ticket_deps
		JOIN tickets ON tickets.id = ticket_deps.depends_on
		WHERE tickets.status <> ?
		ORDER BY ticket_deps.ticket_id, ticket_deps.depends_on`, Done)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	unmet := make(map[int64][]int64)
	for rows.Next() {
		var id, on int64
		if err := rows.Scan(&id, &on); err != nil {
			return nil, err
		}
		unmet[id] = append(unmet[id], on)
	}
	return unmet, rows.Err()
}
