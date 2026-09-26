// Ticket history records each status change rather than keeping event
// timestamps in ticket fields that can become stale.

package store

import (
	"database/sql"
	"time"
)

// addTransition records a status change. An empty before marks ticket
// creation. The caller supplies at so related records, such as a run start
// and its transition, use exactly the same timestamp.
func addTransition(tx *sql.Tx, ticketID int64, before, after TicketStatus, at time.Time) error {
	var from any
	if before != "" {
		from = string(before)
	}
	_, err := tx.Exec(`
		INSERT INTO transitions (ticket_id, from_status, to_status, at)
		VALUES (?, ?, ?, ?)`, ticketID, from, after, rfc3339(at))
	return err
}

// lastChange is a correlated subquery for the latest transition time of the
// outer tickets.id.
const lastChange = `(SELECT at FROM transitions
	 WHERE transitions.ticket_id = tickets.id
	 ORDER BY transitions.id DESC LIMIT 1)`

// acceptedTime selects the transition into Done for the outer tickets.id. It
// controls DONE ordering and its display window. Never-accepted tickets and
// legacy done tickets without acceptance history return NULL.
const acceptedTime = `(SELECT at FROM transitions
	 WHERE transitions.ticket_id = tickets.id AND transitions.to_status = 'done'
	 LIMIT 1)`

// createdTime selects the first history timestamp for the outer tickets.id,
// which records ticket creation.
const createdTime = `(SELECT at FROM transitions
	 WHERE transitions.ticket_id = tickets.id
	 ORDER BY transitions.id LIMIT 1)`
