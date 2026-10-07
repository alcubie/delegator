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

// inboxResultSchema describes inboxJSON for RPC discovery. References keep the
// row contract shared by every group, and additional properties remain valid
// because adding fields does not change jsonSchema.
func inboxResultSchema() map[string]any {
	timestamp := map[string]any{
		"type":   []string{"string", "null"},
		"format": "date-time",
	}
	ticket := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":       map[string]any{"type": "integer"},
			"title":    map[string]any{"type": "string"},
			"status":   map[string]any{"type": "string", "enum": []string{string(store.Queued), string(store.Running), string(store.Ready), string(store.Failed), string(store.Done), string(store.Cancelled)}},
			"project":  map[string]any{"type": "string"},
			"created":  timestamp,
			"accepted": timestamp,
			"started":  timestamp,
		},
		"required":             []string{"id", "title", "status", "project", "created", "accepted", "started"},
		"additionalProperties": true,
	}
	groups := map[string]any{}
	for _, name := range []string{"done", "ready", "running", "failed", "queued"} {
		groups[name] = map[string]any{
			"type":  "array",
			"items": map[string]any{"$ref": "#/definitions/ticket"},
		}
	}
	groups["queue"] = map[string]any{"type": "string", "enum": []string{queueRunning, queuePaused}}
	return map[string]any{
		"type":                 "object",
		"properties":           groups,
		"required":             []string{"queue", "done", "ready", "running", "failed", "queued"},
		"additionalProperties": true,
		"definitions":          map[string]any{"ticket": ticket},
	}
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
