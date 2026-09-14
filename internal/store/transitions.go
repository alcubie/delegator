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

// lastChange is the time of the last change of state of one ticket, which is the
// time that the ticket entered the status it has. It is a sub-query of a query
// over tickets, and tickets.id names the row of that query.
const lastChange = `(SELECT at FROM transitions
	 WHERE transitions.ticket_id = tickets.id
	 ORDER BY transitions.id DESC LIMIT 1)`

// acceptedTime is the time that a ticket became done, which is the time that
// the person accepted the work. DONE comes in the order of it, and the window
// of DONE reads it to decide which tickets the inbox still shows.
//
// Done is the end of the states, so a ticket has at most one change into it. A
// ticket that nobody has accepted gives NULL, and so does a done ticket whose
// history reaches back before the history held this change.
const acceptedTime = `(SELECT at FROM transitions
	 WHERE transitions.ticket_id = tickets.id AND transitions.to_status = 'done'
	 LIMIT 1)`

// createdTime is the time that a ticket arrived, which is the first row of its
// history: the row with no status before it. It is a sub-query of a query over
// tickets, and tickets.id names the row of that query.
const createdTime = `(SELECT at FROM transitions
	 WHERE transitions.ticket_id = tickets.id
	 ORDER BY transitions.id LIMIT 1)`
