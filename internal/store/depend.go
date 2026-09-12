// The links that make one ticket wait for another. A link is explicit, so the
// order that the work has to go in is a fact the database holds and not a
// sentence in the prose that only a person can read.
//
// A link is a row of ticket_deps: (5, 3) says that ticket 5 waits for ticket 3.
// One rule comes out of it, and it is in nextWithRoom: the queue passes over a
// ticket that waits for a ticket that is not done. Nothing else about a link
// constrains the queue, so the place a person gives a ticket is still theirs.

package store

import "database/sql"

// addDependencies writes a link for each id of waitsFor, saying that the ticket
// id waits for it. An id that names no ticket gives ErrNoTicket, so a mistyped
// id stops the write rather than making a link that nothing can ever satisfy.
//
// The same id twice is one row. The primary key of the table is the pair, and
// the caller asked for a state rather than for a count of rows.
func addDependencies(tx *sql.Tx, id int64, waitsFor []int64) error {
	for _, on := range waitsFor {
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
