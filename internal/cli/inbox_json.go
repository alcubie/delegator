// JSON inbox rendering shares grouping and ordering with terminal output
// through internal/inbox.

package cli

import (
	"time"

	"github.com/alcubie/delegator/internal/inbox"
	"github.com/alcubie/delegator/internal/store"
)

// Queue states correspond to the terminal status line.
const (
	queueRunning = "running"
	queuePaused  = "paused"
)

// inboxJSON holds queue state and the same groups as terminal output.
type inboxJSON struct {
	Queue   string            `json:"queue"`
	Done    []inboxTicketJSON `json:"done"`
	Ready   []inboxTicketJSON `json:"ready"`
	Running []inboxTicketJSON `json:"running"`
	Failed  []inboxTicketJSON `json:"failed"`
	Queued  []inboxTicketJSON `json:"queued"`

	box  inbox.Inbox
	now  time.Time
	done time.Duration
}

// inboxTicketJSON represents one inbox row, using the same field names as the
// show result.
type inboxTicketJSON struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Project string `json:"project"`

	// Timestamps use RFC 3339; absent times are null. Started is the
	// latest run start, allowing clients to update elapsed time without
	// fetching a new value every second.
	Created  *time.Time `json:"created"`
	Accepted *time.Time `json:"accepted"`
	Started  *time.Time `json:"started"`
}

// inboxTickets converts a group to JSON rows. Empty groups use [] rather than
// null.
func inboxTickets(tickets []store.OpenTicket) []inboxTicketJSON {
	rows := make([]inboxTicketJSON, 0, len(tickets))
	for _, t := range tickets {
		rows = append(rows, inboxTicketJSON{
			ID:       t.ID,
			Title:    t.Title,
			Status:   string(t.Status),
			Project:  t.Project,
			Created:  nullableTime(t.Created),
			Accepted: nullableTime(t.Accepted),
			Started:  nullableTime(t.Started),
		})
	}
	return rows
}

// queueState names the state of the queue in the word that the document holds.
func queueState(running bool) string {
	if running {
		return queueRunning
	}
	return queuePaused
}

// inboxValue turns the inbox into the value that dg writes. Its unexported
// fields hold the values the text renderer needs alongside the JSON form.
func inboxValue(box inbox.Inbox, now time.Time, done time.Duration) inboxJSON {
	return inboxJSON{
		Queue:   queueState(box.QueueRunning),
		Done:    inboxTickets(box.Done),
		Ready:   inboxTickets(box.Ready),
		Running: inboxTickets(box.Running),
		Failed:  inboxTickets(box.Failed),
		Queued:  inboxTickets(box.Queued),
		box:     box,
		now:     now,
		done:    done,
	}
}
