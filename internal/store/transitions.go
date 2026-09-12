// The history of the state of each ticket. One row of transitions is one change,
// and the status of a ticket is the last row of its history, so no field of a
// ticket holds the time of one change and goes out of date.

package store

import (
	"database/sql"
	"time"
)

// addTransition writes one row of the history of a ticket. before is the empty
// string for the arrival of a ticket, which is the first row of each ticket and
// has no status before it.
//
// The caller gives the time, and a caller that writes a second record of the
// same moment gives the time it wrote there: the claim of a ticket is the start
// of a run, and two clocks would give the two records different times.
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
