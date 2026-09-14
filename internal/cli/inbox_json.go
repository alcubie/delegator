// The inbox for a reader that is not a person. §9.3 says that each command
// which shows data also accepts --json, and §12 says that a GUI in a different
// language uses that flag and needs no Go code.
//
// This file writes the inbox.Inbox structure and decides nothing about which
// ticket goes where: the groups and their order are the answer of
// internal/inbox, and a second answer here would come apart from the one the
// terminal shows.

package cli

import (
	"encoding/json"
	"io"
	"time"

	"github.com/alcubie/delegator/internal/inbox"
	"github.com/alcubie/delegator/internal/store"
)

// The value of queue, which is the state that the first line of the text form
// says in words.
const (
	queueRunning = "running"
	queuePaused  = "paused"
)

// inboxJSON is the whole inbox as one object. It holds one key for each group
// of inbox.Inbox, in the order that the text form writes them, so a reader of
// the two sees one list.
type inboxJSON struct {
	Queue   string            `json:"queue"`
	Done    []inboxTicketJSON `json:"done"`
	Ready   []inboxTicketJSON `json:"ready"`
	Running []inboxTicketJSON `json:"running"`
	Failed  []inboxTicketJSON `json:"failed"`
	Queued  []inboxTicketJSON `json:"queued"`
}

// inboxTicketJSON is one row of the inbox. Each key that names a field of
// dg show --json is the same word there, so a reader of the two knows the
// field without a second table.
type inboxTicketJSON struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Project string `json:"project"`

	// The times are RFC 3339, and a time the ticket has not reached is null.
	// Started is the start of the last run, which for a ticket in running is
	// the run that holds it and the moment a GUI counts the elapsed time from.
	// No key holds that count: a string made here stops as soon as the reader
	// draws it.
	Created  *time.Time `json:"created"`
	Accepted *time.Time `json:"accepted"`
	Started  *time.Time `json:"started"`
}

// inboxTickets turns one group into its rows. A group that holds no ticket is
// an empty list and not null, so a reader walks each group the one way.
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

// writeInboxJSON writes the inbox as one JSON object and nothing else.
func writeInboxJSON(out io.Writer, box inbox.Inbox) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	// The title is what the person wrote, and the escape of `<`, `>` and `&`
	// is for JSON that goes inside a page. Nothing here is a page.
	enc.SetEscapeHTML(false)
	return enc.Encode(inboxJSON{
		Queue:   queueState(box.QueueRunning),
		Done:    inboxTickets(box.Done),
		Ready:   inboxTickets(box.Ready),
		Running: inboxTickets(box.Running),
		Failed:  inboxTickets(box.Failed),
		Queued:  inboxTickets(box.Queued),
	})
}
