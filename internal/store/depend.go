// The links that make one ticket depend on another. A link is explicit, so the
// order that the work has to go in is a fact the database holds and not a
// sentence in the prose that only a person can read.
//
// A link is a row of ticket_deps: (5, 3) says that ticket 5 depends on ticket 3.
// One rule comes out of it, and it is in nextWithRoom: the queue passes over a
// ticket that depends on a ticket that is not done. Nothing else about a link
// constrains the queue, so the place a person gives a ticket is still theirs.
//
// Two reads are here as well. unmetDependencies answers, for the whole inbox at
// once, which links still hold a ticket back, and Dependencies gives every link
// of one ticket for dg show.

package store

import "database/sql"

// addDependencies writes a link for each id of dependsOn, saying that the
// ticket id depends on it. An id that names no ticket gives ErrNoTicket, so a
// mistyped id stops the write rather than making a link that nothing can ever
// satisfy.
//
// The same id twice is one row. The primary key of the table is the pair, and
// the caller asked for a state rather than for a count of rows.
func addDependencies(tx *sql.Tx, id int64, dependsOn []int64) error {
	for _, on := range dependsOn {
		// Checked here so a missing id returns ErrNoTicket naming that id.
		// The INSERT below would instead fail with SQLite error 787,
		// "FOREIGN KEY constraint failed", which names no id.
		if err := ticketExists(tx, on); err != nil {
			return err
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
