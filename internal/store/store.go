// Package store keeps each field of delegator in one SQLite database. The
// prose of a ticket is not here: the person writes the prose with an editor,
// and an editor opens a file and not a row.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"time"

	// The blank name imports this package for its init function only. The init
	// function puts the driver in the registry of database/sql below the name
	// "sqlite", and each call of this package goes through database/sql.
	_ "modernc.org/sqlite"
)

// dbFile is the name of the database below the data directory. A change of
// this name loses the data of each person who has delegator now.
const dbFile = "delegator.db"

// dirPerm gives the permission of the data directory and of each directory
// below it. A ticket can contain private data, and a directory with no
// permission for a group or for other persons stops each other person on the
// computer. Delegator therefore gives the permission in this one place, and no
// command gives a permission to the file that it writes.
const dirPerm = 0o700

// dataDirs are the directories that hold the files. The prose of each ticket is
// in tickets, the copy of a repository for each run is in worktrees, and the
// log of each agent is in runs.
var dataDirs = []string{"", "tickets", "worktrees", "runs"}

// busyTimeout is the time in milliseconds that a connection waits for the lock
// of a writer before it gives an error. Delegator has no server, so two
// commands of the person, or a command and a supervisor, can want the lock at
// the same time. A wait is correct here, and an error is not.
const busyTimeout = 5000

// dsn gives the name that sql.Open takes. Each setting is in the name, and not
// in a statement after the open, because database/sql keeps a pool of
// connections and makes a new one at any time. A statement after the open
// reaches one connection only.
//
// The path goes in a URL, so a path that holds ? or # needs an escape. Without
// it SQLite reads the path only as far as that character, and it makes the
// database at a different place and gives no error. url.URL escapes the path
// and keeps each separator of a directory.
//
// _txlock=immediate makes each transaction that writes take the lock of the
// writer at its BEGIN. A transaction that takes the lock later, after it reads,
// gives SQLITE_BUSY the moment another writer has the lock, and busy_timeout
// does not wait: SQLite cannot let a reader wait to become a writer. A
// transaction that database/sql opens with ReadOnly keeps a plain BEGIN, so a
// program that only reads does not wait.
func dsn(dataDir string) string {
	u := url.URL{
		Scheme: "file",
		Path:   filepath.Join(dataDir, dbFile),
		RawQuery: fmt.Sprintf(
			"_pragma=journal_mode(WAL)&_pragma=busy_timeout(%d)&_pragma=foreign_keys(on)&_txlock=immediate",
			busyTimeout,
		),
	}
	return u.String()
}

// Store holds the open database. Each command makes one Store, and closes it
// when the command stops.
type Store struct {
	db *sql.DB
}

// TicketStatus is the state of a ticket.  The constants below are the states.
type TicketStatus string

// The states of a ticket, and what changes each one:
//
//	(new)                  -> queued     dg ticket, at the end of the queue
//	queued                 -> running    no command. A supervisor takes the first ticket.
//	running                -> ready      dg finish, from the agent
//	running                -> failed     the timeout, an error, or the end before dg finish
//	failed                 -> queued     dg restart, with the same session and worktree
//	ready                  -> done       dg accept, which removes the worktree
//	ready                  -> queued     dg revise, at the end of the queue again
//	any non-terminal state -> cancelled  dg cancel
//
// done and cancelled are the end. Only dg finish gives ready, so a run that
// stops early cannot look complete.
//
// See TECHNICAL_DESIGN.md Section 8 for a visual
const (
	Queued    TicketStatus = "queued"
	Running   TicketStatus = "running"
	Ready     TicketStatus = "ready"
	Failed    TicketStatus = "failed"
	Done      TicketStatus = "done"
	Cancelled TicketStatus = "cancelled"
)

// nextStates gives each state that one state can go to.
// See Section 8 of the technical document for a visual diagram
var nextStates = map[TicketStatus][]TicketStatus{
	Queued:    {Running, Cancelled},
	Running:   {Ready, Failed, Cancelled},
	Ready:     {Done, Queued, Cancelled},
	Failed:    {Queued, Cancelled},
	Done:      nil,
	Cancelled: nil,
}

// canChange gives the ok to whether one TicketStatus can advance to another TicketStatus.
func canChange(from, to TicketStatus) bool {
	return slices.Contains(nextStates[from], to)
}

// ErrInvalidTicketStateChange shows that an invalid state change was attempted and blocked.
var ErrInvalidTicketStateChange = errors.New("invalid ticket state change")

// ErrNotInTheQueue shows that a ticket is not one that the queue holds. The id
// can belong to no ticket, or to a ticket that has a status other than queued.
var ErrNotInTheQueue = errors.New("the ticket is not in the queue")

// Open gives the database below dataDir. It makes the data directory, the
// directories below it, and the database, if they are not present.
func Open(dataDir string) (*Store, error) {
	// The directories come first. SQLite cannot make its file below a directory
	// that is not present.
	for _, name := range dataDirs {
		if err := os.MkdirAll(filepath.Join(dataDir, name), dirPerm); err != nil {
			return nil, err
		}
	}

	db, err := sql.Open("sqlite", dsn(dataDir))
	if err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close stops the connection to the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// create executes the query SQL and returns the inserted ID.
func (s *Store) create(query string, args ...any) (int64, error) {
	result, err := s.db.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	return id, nil
}

// AddProject adds a new project record to the database.
func (s *Store) AddProject(path, defaultBranch string) (int64, error) {
	query := `INSERT INTO projects (
		path, default_branch
	) VALUES (?, ?)`

	args := []any{
		path,
		defaultBranch,
	}

	return s.create(query, args...)
}

// AddTicket adds a new ticket record to the database.
func (s *Store) AddTicket(projectID int64, title string) (int64, error) {
	// The position comes from a sub-query in the same statement, so the read of
	// the last position and the write of the new one cannot come apart. A
	// ticket that delegator makes is in the queue, which is what the state
	// "queued" says.
	query := `INSERT INTO tickets (
		project_id, title, status, position, created
	) VALUES (?, ?, ?, (SELECT COALESCE(MAX(position), 0) + 1 FROM tickets), ?)
	`
	args := []any{
		projectID,
		title,
		Queued,
		time.Now().UTC().Format(time.RFC3339),
	}

	return s.create(query, args...)
}

// QueuedTicket is one ticket of the queue. It holds the fields that the queue
// shows, and not each field of the row.
type QueuedTicket struct {
	ID    int64
	Title string
}

// ListQueue gives each ticket of the queue, in the sequence of position.
//
// A ticket of the queue holds the status queued and a position. Each one alone
// is not enough: a ticket with a position and another status left the queue,
// and a ticket with the status queued and no position is in no queue at all.
func (s *Store) ListQueue() ([]QueuedTicket, error) {
	rows, err := s.db.Query(`
		SELECT id, title FROM tickets
		WHERE status = ? AND position IS NOT NULL
		ORDER BY position`, Queued)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var queue []QueuedTicket
	for rows.Next() {
		var t QueuedTicket
		if err := rows.Scan(&t.ID, &t.Title); err != nil {
			return nil, err
		}
		queue = append(queue, t)
	}
	// A loop over rows stops on an error as well as on the last row, and Next
	// gives false for both. Err tells the two apart.
	return queue, rows.Err()
}

// RemoveTicket removes ticket with ID id from the queue.
// The positions of the other tickets are not modified as they will still remain in the correct order.
// The ticket must have its status as "queued" and a position in the queue in order to be modified.
func (s *Store) RemoveTicket(id int64, newStatus TicketStatus) (bool, error) {
	if !canChange(Queued, newStatus) {
		return false, fmt.Errorf("%w: %s to %s", ErrInvalidTicketStateChange, Queued, newStatus)
	}

	result, err := s.db.Exec(
		`UPDATE tickets
			SET status = ?, position = NULL
			WHERE id = ? AND status = ? AND position IS NOT NULL
		`,
		newStatus,
		id,
		Queued,
	)
	if err != nil {
		return false, err
	}

	num, err := result.RowsAffected()
	if err != nil {
		return false, err
	}

	return num == 1, nil
}

// MoveTicket moves one ticket in the queue, in the direction of move. The read
// of the sequence and the write of each new position are in one transaction, so
// the sequence that moves is the sequence that the queue has.
func (s *Store) MoveTicket(id int64, move Move) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	ids, err := queuedIDs(tx)
	if err != nil {
		return err
	}
	from := slices.Index(ids, id)
	if from < 0 {
		return fmt.Errorf("%w: ticket %d", ErrNotInTheQueue, id)
	}

	if err := setPositions(tx, reorder(ids, from, move)); err != nil {
		return err
	}
	return tx.Commit()
}

// queuedIDs gives the id of each ticket of the queue, in the sequence of
// position. It asks the same question as ListQueue, so a move operates on the
// queue that the person can see.
func queuedIDs(tx *sql.Tx) ([]int64, error) {
	rows, err := tx.Query(`
		SELECT id FROM tickets
		WHERE status = ? AND position IS NOT NULL
		ORDER BY position`, Queued)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// setPositions writes the sequence of the queue. The first id of ids takes
// position 1, the next takes 2, and so on.
func setPositions(tx *sql.Tx, ids []int64) error {
	// The column has a unique index, and a ticket can take a position that
	// another ticket holds now, so each position goes below zero first. A
	// position that is not there at all would be simpler, but the CHECK of the
	// table refuses a queued ticket with no position, and SQLite has no CHECK
	// that waits for the commit.
	if _, err := tx.Exec("UPDATE tickets SET position = -position WHERE position IS NOT NULL"); err != nil {
		return err
	}
	for i, id := range ids {
		if _, err := tx.Exec("UPDATE tickets SET position = ? WHERE id = ?", i+1, id); err != nil {
			return err
		}
	}
	return nil
}
