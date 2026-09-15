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

	"github.com/alcubie/delegator/internal/config"

	// The blank name imports this package for its init function only. The init
	// function puts the driver in the registry of database/sql below the name
	// "sqlite", and each call of this package goes through database/sql.
	_ "modernc.org/sqlite"
)

// dbFile is the name of the database below the data directory. A change of
// this name loses the data of each person who has delegator now.
const dbFile = "delegator.db"

// dirPerm is the permission of the data directory and of each directory
// below it. A ticket can contain private data, and a directory with no
// permission for a group or for other persons stops each other person on the
// computer. Delegator therefore gives the permission in this one place, and no
// command gives a permission to the file that it writes.
const dirPerm = 0o700

// filePerm is the permission of the database. A ticket can hold private
// data, so only the person who made it can read it. Open makes the file before
// SQLite does, for two reasons: SQLite makes it with the umask of the person,
// and SQLite gives each write-ahead file the permission that the database has.
// A chmod after the open therefore leaves delegator.db-wal open to each other
// person, and the write-ahead file holds the newest writes.
const filePerm = 0o600

// dataDirs are the directories that hold the files. The prose of each ticket is
// in tickets, the copy of a repository for each run is in worktrees, and the
// log of each agent is in runs.
var dataDirs = []string{"", "tickets", "worktrees", "runs"}

// busyTimeout is the time in milliseconds that a connection waits for the lock
// of a writer before it gives an error. Delegator has no server, so two
// commands of the person, or a command and a supervisor, can want the lock at
// the same time. A wait is correct here, and an error is not.
const busyTimeout = 5000

// dsn returns the name that sql.Open takes. Each setting is in the name, and not
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

// rfc3339 returns a time as the store holds it: RFC 3339 in UTC, to the
// second, in a TEXT column, because SQLite has no type for a time. A time in
// that form sorts as text in the order of time.
func rfc3339(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// timeColumn scans one TEXT column that holds a time in the form of rfc3339
// into a time.Time. NULL gives the zero time, which no row can hold, so it
// stands for no time the way the zero value of each other type stands for
// NULL.
type timeColumn struct {
	t *time.Time
}

func (c timeColumn) Scan(v any) error {
	var text string
	switch v := v.(type) {
	case nil:
		*c.t = time.Time{}
		return nil
	case string:
		text = v
	case []byte:
		text = string(v)
	default:
		return fmt.Errorf("a time column holds %T", v)
	}
	t, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return err
	}
	*c.t = t
	return nil
}

// querier is the part of *sql.DB and *sql.Tx that a read needs, so one query
// serves a caller inside a transaction and a caller outside one.
type querier interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// Store holds the open database. Each command makes one Store, and closes it
// when the command stops.
type Store struct {
	db  *sql.DB
	dir string
}

// DataDir returns the directory the store was opened in. The database, the
// worktrees, the runs, and the ticket prose all live below it.
func (s *Store) DataDir() string {
	return s.dir
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

// nextStates holds each state that one state can go to.
// See Section 8 of the technical document for a visual diagram
var nextStates = map[TicketStatus][]TicketStatus{
	Queued:    {Running, Cancelled},
	Running:   {Ready, Failed, Cancelled},
	Ready:     {Done, Queued, Cancelled},
	Failed:    {Queued, Cancelled},
	Done:      nil,
	Cancelled: nil,
}

// canChange reports whether one status can change to another.
func canChange(from, to TicketStatus) bool {
	return slices.Contains(nextStates[from], to)
}

// ErrInvalidTicketStateChange shows that an invalid state change was attempted and blocked.
var ErrInvalidTicketStateChange = errors.New("invalid ticket state change")

// ErrNotMovable shows that a move cannot take a ticket, because the ticket is
// in no list that has a sequence. The id can belong to no ticket, or to a ticket
// that is running, done, failed or cancelled.
var ErrNotMovable = errors.New("only a queued ticket and a ready ticket can move")

// ErrNotInTheSameList shows that the target of a move is not in the list that
// the ticket is in. A move orders one list, so a ready ticket cannot go among the
// tickets of the queue and a queued ticket cannot go among the ready ones.
var ErrNotInTheSameList = errors.New("a ticket moves inside its own list")

// Open returns the database below dataDir. It makes the data directory, the
// directories below it, and the database, if they are not present.
func Open(dataDir string) (*Store, error) {
	// The directories come first. SQLite cannot make its file below a directory
	// that is not present.
	for _, name := range dataDirs {
		if err := os.MkdirAll(filepath.Join(dataDir, name), dirPerm); err != nil {
			return nil, err
		}
	}

	f, err := os.OpenFile(filepath.Join(dataDir, dbFile), os.O_CREATE|os.O_RDWR, filePerm)
	if err != nil {
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", dsn(dataDir))
	if err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, dir: dataDir}, nil
}

// With opens the store at dataDir, calls fn with it, and closes it. A command
// then cannot forget the close, and an error from Close is not lost the way a
// deferred Close loses it: fn's error wins, and Close's is returned when fn
// had none.
func With(dataDir string, fn func(*Store) error) error {
	s, err := Open(dataDir)
	if err != nil {
		return err
	}
	if err := fn(s); err != nil {
		s.Close()
		return err
	}
	return s.Close()
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

// Project is one row of the table projects.
type Project struct {
	ID            int64
	Path          string
	DefaultBranch string
}

// ProjectID returns the id of the project at path, and makes the row if the path
// is not there. The first ticket of a project therefore settles its default
// branch, and a later ticket of the same project keeps it.
func (s *Store) ProjectID(path, defaultBranch string) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var id int64
	err = tx.QueryRow("SELECT id FROM projects WHERE path = ?", path).Scan(&id)
	switch {
	case err == nil:
		return id, tx.Commit()
	case !errors.Is(err, sql.ErrNoRows):
		return 0, err
	}

	result, err := tx.Exec(
		"INSERT INTO projects (path, default_branch) VALUES (?, ?)", path, defaultBranch)
	if err != nil {
		return 0, err
	}
	if id, err = result.LastInsertId(); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// Projects returns each project, in the order of id.
func (s *Store) Projects() ([]Project, error) {
	rows, err := s.db.Query("SELECT id, path, default_branch FROM projects ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Path, &p.DefaultBranch); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

// AddTicket adds a new ticket record to the database. The ids of dependsOn name
// the tickets that the new one depends on: the queue passes it over until each
// of them is done. An id that names no ticket gives ErrNoTicket and leaves the
// database as it was.
//
// The ticket, its links and the first row of its history go in under one
// transaction, so no supervisor can claim the ticket in the moment between the
// writes and start work that a link says must wait.
//
// The arrival of the ticket is that first row of the history, and no column of
// the ticket holds it as well.
func (s *Store) AddTicket(projectID int64, title string, dependsOn ...int64) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// The position comes from a sub-query in the same statement, so the read of
	// the last position and the write of the new one cannot come apart. A
	// ticket that delegator makes is in the queue, which is what the state
	// "queued" says.
	arrived := time.Now()
	result, err := tx.Exec(`INSERT INTO tickets (
		project_id, title, status, position
	) VALUES (?, ?, ?, (SELECT COALESCE(MAX(position), 0) + 1 FROM tickets))
	`, projectID, title, Queued)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	if err := addTransition(tx, id, "", Queued, arrived); err != nil {
		return 0, err
	}
	if err := addDependencies(tx, id, dependsOn); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// QueuedTicket is one ticket of the queue. It holds the fields that the queue
// shows, and not each field of the row.
type QueuedTicket struct {
	ID    int64
	Title string
}

// ListQueue returns each ticket of the queue, in the order of position.
//
// A ticket of the queue holds the status queued and a position. Each one alone
// is not enough: a ticket with a position and another status left the queue,
// and a ticket with the status queued and no position is in no queue at all.
func (s *Store) ListQueue() ([]QueuedTicket, error) {
	return listQueue(s.db)
}

// listQueue is ListQueue for any querier, so the claim of a supervisor reads
// the queue inside its own transaction and no second query has to say what a
// ticket of the queue is.
func listQueue(q querier) ([]QueuedTicket, error) {
	rows, err := q.Query(`
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

// list is a group of the inbox that holds its tickets in an order the person
// sets, with the column that keeps the place of a ticket in it. The queue is
// one list and READY is the other, and a move orders the tickets of one of them.
//
// The status and the column go together, so a caller that knows the list cannot
// read one of them and write the place of the other.
type list struct {
	status TicketStatus
	column string
}

// lists holds each list that a move can order.
var lists = []list{
	{Queued, "position"},
	{Ready, "ready_position"},
}

// MoveTicket moves one ticket inside its list, in the direction of move. The
// read of the order and the write of each new position are in one transaction,
// so the order that moves is the order that the list has.
func (s *Store) MoveTicket(id int64, move Move) error {
	return s.move(id, func(_ list, ids []int64, from int) ([]int64, error) {
		return reorder(ids, from, move), nil
	})
}

// MoveTicketBefore puts one ticket where target is, and target and each ticket
// below it go down one place. A person who moves a ticket into the middle of a
// list therefore writes one command, and not one dg move up for each place.
//
// A target that is the ticket itself changes nothing, because a ticket is
// already where it is.
func (s *Store) MoveTicketBefore(id, target int64) error {
	return s.move(id, func(where list, ids []int64, from int) ([]int64, error) {
		if !slices.Contains(ids, target) {
			return nil, fmt.Errorf("%w: ticket %d is not %s", ErrNotInTheSameList, target, where.status)
		}
		return reorderBefore(ids, from, target), nil
	})
}

// move reads the order of the list that holds the ticket, gives it to order, and
// writes what comes back. The read and the write are below one transaction, so
// the order that moves is the order that the list has.
func (s *Store) move(id int64, order func(where list, ids []int64, from int) ([]int64, error)) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	where, err := listOf(tx, id)
	if err != nil {
		return err
	}
	ids, err := listIDs(tx, where)
	if err != nil {
		return err
	}
	from := slices.Index(ids, id)
	if from < 0 {
		return fmt.Errorf("%w: ticket %d", ErrNotMovable, id)
	}

	moved, err := order(where, ids, from)
	if err != nil {
		return err
	}
	if err := setPositions(tx, where, moved); err != nil {
		return err
	}
	return tx.Commit()
}

// listOf returns the list that holds the ticket, and ErrNotMovable when no
// list does. A ticket that no row holds gives that error as well: the answer to
// dg move is the same either way, that the ticket is not one a move can take.
func listOf(tx *sql.Tx, id int64) (list, error) {
	var status TicketStatus
	err := tx.QueryRow("SELECT status FROM tickets WHERE id = ?", id).Scan(&status)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return list{}, err
	}
	for _, l := range lists {
		if l.status == status {
			return l, nil
		}
	}
	return list{}, fmt.Errorf("%w: ticket %d", ErrNotMovable, id)
}

// listIDs returns the id of each ticket of a list, in the order of its column of
// positions. For the queue it asks the same question as ListQueue, so a move
// operates on the order that the person can see.
func listIDs(tx *sql.Tx, l list) ([]int64, error) {
	rows, err := tx.Query(fmt.Sprintf(`
		SELECT id FROM tickets
		WHERE status = ? AND %[1]s IS NOT NULL
		ORDER BY %[1]s`, l.column), l.status)
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

// setPositions writes the order of one list. The first id of ids takes
// position 1, the next takes 2, and so on.
func setPositions(tx *sql.Tx, l list, ids []int64) error {
	// The column has a unique index, and a ticket can take a position that
	// another ticket holds now, so each position goes below zero first. A
	// position that is not there at all would be simpler, but the CHECK of the
	// table refuses a queued ticket with no position, and SQLite has no CHECK
	// that waits for the commit.
	if _, err := tx.Exec(fmt.Sprintf(
		"UPDATE tickets SET %[1]s = -%[1]s WHERE %[1]s IS NOT NULL", l.column)); err != nil {
		return err
	}
	for i, id := range ids {
		if _, err := tx.Exec(fmt.Sprintf(
			"UPDATE tickets SET %s = ? WHERE id = ?", l.column), i+1, id); err != nil {
			return err
		}
	}
	return nil
}

// OpenTicket is one ticket of the inbox. The project is the path of the
// repository, and the inbox makes the short name that it shows from it.
//
// A column that holds NULL arrives here as the zero value of its type, and not
// as a sql.Null. The rule is that the zero value stands for NULL when no row
// can hold that value: a position starts at 1, the zero time is no time, and
// the empty string is no branch. A column that can hold its zero value would
// need a sql.Null, to keep the two apart. The status says which group the ticket is in, so no field here
// must answer that as well.
type OpenTicket struct {
	ID      int64
	Project string
	Title   string
	Status  TicketStatus

	// Position is the place of the ticket in its list: the queue for a queued
	// ticket and READY for a ready one. A ticket of neither list holds 0, which
	// is no place, and Status says which list the ticket is in.
	Position int

	// Created is the time that the ticket arrived, which is the first row of
	// its history. Every ticket has one.
	Created time.Time

	// Accepted is the time that the person accepted the ticket, which is its
	// change into done. It orders DONE and sets the window that DONE holds. A
	// ticket that nobody has accepted holds the zero time, which every ticket of
	// the open list does.
	Accepted time.Time

	// Started is the time that the last run of the ticket began. For a ticket
	// in running that is the run that holds it, and the inbox takes the
	// duration of the run from it. A ticket that no supervisor has claimed
	// holds the zero time.
	Started time.Time

	// Changed is the time of the last change of state, which is the time that
	// the ticket entered the status it has. A ticket in failed entered failed
	// then, which is the time of the failure, and FAILED comes in the order of
	// it. A ticket whose history holds no change holds the zero time.
	Changed time.Time

	// DependsOn holds the id of each ticket that this one depends on and that is
	// not done yet, in the order of the ids. A ticket that depends on nothing
	// holds none, so the inbox writes a note about a link only while the link
	// still holds the ticket back.
	DependsOn []int64
}

// OpenTickets returns each ticket that is still open: the ones that wait, the
// one that runs, the ones that are complete and wait for the person, and the
// ones that failed. A ticket that is done or cancelled is closed, and
// DoneTickets returns the ones of those that the inbox still shows.
//
// One query returns the tickets of each project, because the inbox is one list
// for all projects.
func (s *Store) OpenTickets() ([]OpenTicket, error) {
	return s.inboxTickets(inboxTicketQuery+`
		WHERE tickets.status IN (?, ?, ?, ?)
		ORDER BY tickets.id`, Queued, Running, Ready, Failed)
}

// AllTickets returns every ticket, in the order of the ids, whatever its
// status. project is the path of the one project the list holds, and the empty
// string is every project. dg list reads it.
//
// The order is the order the tickets were made in, and it is not the order of
// the inbox: the inbox is the work of a day in the order a person deals with
// it, and this list is the record of every ticket there has ever been, which a
// person reads by number.
func (s *Store) AllTickets(project string) ([]OpenTicket, error) {
	if project == "" {
		return s.inboxTickets(inboxTicketQuery + `ORDER BY tickets.id`)
	}
	return s.inboxTickets(inboxTicketQuery+`
		WHERE projects.path = ?
		ORDER BY tickets.id`, project)
}

// DoneTickets returns each ticket that dg accept closed at or after since. The
// inbox holds them for a while after they close, so a person who accepted a
// ticket can still read what it was.
//
// The window runs from the time the person accepted the ticket and not from the
// time the run finished. A ticket that waits in ready over a weekend is work the
// person has just dealt with on the Monday, and DONE holds it for the window
// from that moment.
//
// A cancelled ticket is not one of these, because a cancel is not an
// acceptance. Neither is a done ticket whose history holds no change into done,
// which is every ticket that a version before the history had already closed.
func (s *Store) DoneTickets(since time.Time) ([]OpenTicket, error) {
	// The time holds the form of rfc3339 in UTC, which is one width and one
	// zone for every row, so a comparison of the text is a comparison of the
	// times.
	return s.inboxTickets(inboxTicketQuery+`
		WHERE tickets.status = ? AND `+acceptedTime+` >= ?
		ORDER BY tickets.id`, Done, rfc3339(ceilSecond(since)))
}

// ceilSecond rounds a time up to the next whole second, and leaves a time that
// is already whole as it is.
//
// The time of a change names a second and no part of one, so the window rounds
// the same way: a ticket is in it only when the whole second that its time
// names is. Without this a window of no length would still hold each ticket that
// finished in the second it began in, and a person who asked for no DONE at
// all would see one.
func ceilSecond(t time.Time) time.Time {
	whole := t.Truncate(time.Second)
	if whole.Equal(t) {
		return t
	}
	return whole.Add(time.Second)
}

// inboxTicketQuery selects the columns of an OpenTicket, and a caller adds the
// WHERE and the ORDER BY that pick its rows.
//
// The start of the run comes from a sub-query and not from a join, because a
// ticket has a row of runs for each claim and a join would give a ticket once
// for each of them.
const inboxTicketQuery = `
	SELECT tickets.id, projects.path, tickets.title, tickets.status,
	       COALESCE(tickets.position, tickets.ready_position, 0),
	       ` + createdTime + `, ` + acceptedTime + `,
	       (SELECT started_at FROM runs
	        WHERE runs.ticket_id = tickets.id ORDER BY runs.id DESC LIMIT 1),
	       ` + lastChange + `
	FROM tickets
	JOIN projects ON projects.id = tickets.project_id
	`

// inboxTickets runs a query that inboxTicketQuery starts and reads each row of
// it into an OpenTicket. Store.Ticket does not use it: that one reads the
// branch, the session and the commit as well, which no row of the inbox shows.
func (s *Store) inboxTickets(query string, args ...any) ([]OpenTicket, error) {
	// The links come first, in one query for every ticket. A query for each
	// row would read the links inside the loop over the rows, which is one
	// round trip for each ticket of the queue.
	unmet, err := unmetDependencies(s.db)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var open []OpenTicket
	for rows.Next() {
		var t OpenTicket
		if err := rows.Scan(
			&t.ID, &t.Project, &t.Title, &t.Status, &t.Position,
			timeColumn{&t.Created}, timeColumn{&t.Accepted},
			timeColumn{&t.Started}, timeColumn{&t.Changed}); err != nil {
			return nil, err
		}
		t.DependsOn = unmet[t.ID]
		open = append(open, t)
	}
	return open, rows.Err()
}

// ErrNoTicket shows that no ticket holds the id.
var ErrNoTicket = errors.New("no such ticket")

// Ticket is one row of the table tickets, with the project it belongs to. A
// NULL column arrives as the zero value of its type, as it does for an
// OpenTicket.
type Ticket struct {
	ID       int64
	Project  Project
	Title    string
	Status   TicketStatus
	Position int
	Branch   string
	Session  string
	Commit   string

	// Created is the time that the ticket arrived, which is the first row of
	// its history.
	Created time.Time

	// Accepted is the time that the person accepted the ticket, and the zero
	// time is a ticket that nobody has accepted.
	Accepted time.Time

	// Changed is the time of the last change of state, which is the time that
	// the ticket entered the status it has.
	Changed time.Time
}

// Ticket returns one ticket. It gives ErrNoTicket if the id holds none.
func (s *Store) Ticket(id int64) (Ticket, error) {
	return ticket(s.db, id)
}

// ticketExists gives ErrNoTicket when the id holds no ticket, and nothing when
// it holds one. It reads no column, because a caller that asks only whether the
// id is there has no use for the join to projects and the fields that ticket
// reads.
func ticketExists(q querier, id int64) error {
	var found int
	err := q.QueryRow("SELECT 1 FROM tickets WHERE id = ?", id).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %d", ErrNoTicket, id)
	}
	return err
}

// statusOf returns the status of one ticket, and ErrNoTicket when the id holds
// none. A caller that decides on the status writes in the same transaction, so
// the status it read is the status it writes against.
func statusOf(q querier, id int64) (TicketStatus, error) {
	var status TicketStatus
	err := q.QueryRow("SELECT status FROM tickets WHERE id = ?", id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%w: %d", ErrNoTicket, id)
	}
	return status, err
}

// ticket is Ticket for any querier.
func ticket(q querier, id int64) (Ticket, error) {
	var t Ticket
	err := q.QueryRow(`
		SELECT tickets.id, projects.id, projects.path, projects.default_branch,
		       tickets.title, tickets.status,
		       COALESCE(tickets.position, 0), COALESCE(tickets.branch, ''),
		       COALESCE(tickets.session, ''), COALESCE(tickets.commit_id, ''),
		       `+createdTime+`, `+acceptedTime+`, `+lastChange+`
		FROM tickets
		JOIN projects ON projects.id = tickets.project_id
		WHERE tickets.id = ?`, id).Scan(
		&t.ID, &t.Project.ID, &t.Project.Path, &t.Project.DefaultBranch,
		&t.Title, &t.Status, &t.Position, &t.Branch,
		&t.Session, &t.Commit,
		timeColumn{&t.Created}, timeColumn{&t.Accepted}, timeColumn{&t.Changed})
	if errors.Is(err, sql.ErrNoRows) {
		return Ticket{}, fmt.Errorf("%w: %d", ErrNoTicket, id)
	}
	if err != nil {
		return Ticket{}, err
	}
	return t, nil
}

// ChangeStatus sets a new status for a ticket. It reads the current status
// first, so nextStates decides whether the change is allowed, and returns
// ErrInvalidTicketStateChange if it is not, and ErrNoTicket if no ticket has
// the id.
//
// A position follows status: entering queued or ready appends the ticket to the
// end of that list, and leaving a list clears the position it held, so the CHECK
// constraint holds either way and no ticket keeps a place in a list it left.
//
// The read and the write share one transaction, which is BEGIN IMMEDIATE, so
// nothing can change the status in between.
func (s *Store) ChangeStatus(id int64, status TicketStatus) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := changeStatus(tx, id, status, time.Now()); err != nil {
		return err
	}
	return tx.Commit()
}

// changeStatus is ChangeStatus without the transaction, so a caller that writes
// more than the status can do all of it in one. It writes the row of the change
// as well, so no way to change a status can leave the history without it, and at
// is the time that both hold.
func changeStatus(tx *sql.Tx, id int64, status TicketStatus, at time.Time) error {
	from, err := statusOf(tx, id)
	if err != nil {
		return err
	}
	if !canChange(from, status) {
		return fmt.Errorf("%w: %s to %s", ErrInvalidTicketStateChange, from, status)
	}

	update := "UPDATE tickets SET status = ?, position = NULL, ready_position = NULL WHERE id = ?"
	switch status {
	case Queued:
		update = `UPDATE tickets
			SET status = ?, position = (SELECT COALESCE(MAX(position), 0) + 1 FROM tickets),
			    ready_position = NULL
			WHERE id = ?
		`
	case Ready:
		update = `UPDATE tickets
			SET status = ?, position = NULL,
			    ready_position = (SELECT COALESCE(MAX(ready_position), 0) + 1 FROM tickets)
			WHERE id = ?
		`
	}
	if _, err := tx.Exec(update, status, id); err != nil {
		return err
	}
	return addTransition(tx, id, from, status, at)
}

// Claim marks a queued ticket as running, records its branch, and writes the
// row of runs for this run, all in one transaction. The row holds the process
// id of the caller, which is the supervisor, and the time of the claim, so a
// ticket in running always has a process id to ask about. A second supervisor
// racing for the same ticket finds it already running, gets
// ErrInvalidTicketStateChange, and writes nothing.
//
// It returns the id of the row it wrote, which is the run the caller holds.
// Anything the caller writes on that run later names the id, because the last
// run of a ticket is not always this one: dg restart takes a ticket that
// failed and claims it again, and a supervisor still working on the run before
// it would otherwise write on the row of the new one.
//
// Create the worktree before calling this, never inside it: the transaction
// holds SQLite's writer lock, and git worktree add runs long enough to park
// every other dg command on the busy_timeout.
func (s *Store) Claim(id int64, branch string) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	runID, err := claim(tx, id, branch)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return runID, nil
}

// ErrNoRoom shows that the queue had no ticket for a supervisor: the queue is
// empty or paused, or the tickets in running and in ready fill every slot the
// limit of the person allows. It is the ordinary end of a supervisor that a
// trigger started for a queue that has since filled its slots, and not a
// fault.
var ErrNoRoom = errors.New("no ticket in the queue has room to run")

// freeSlots reports how many runs can start: cfg.Runs, the limit of the
// person, less the tickets that hold a slot. A ticket in running holds one
// because its supervisor is working on it, and a ticket in ready holds one
// because the person has not examined that work yet. A paused queue has no
// free slot whatever the limit, and a limit the tickets have already passed
// gives none rather than a count below zero. An empty queue is not this
// question; it is what the queue itself says.
func freeSlots(q querier, cfg config.Config) (int, error) {
	var running bool
	var held int
	err := q.QueryRow(`
		SELECT (SELECT running FROM queue_state WHERE id = 1),
		       (SELECT COUNT(*) FROM tickets WHERE status IN (?, ?))`,
		Running, Ready).Scan(&running, &held)
	if err != nil {
		return 0, err
	}
	if !running {
		return 0, nil
	}
	return max(cfg.Runs-held, 0), nil
}

// FreeSlots reports how many runs the queue has room for. A trigger asks it to
// decide how many supervisors to start; the claim of each supervisor asks it
// again inside its own transaction, which is the answer that counts.
//
// It takes the whole config, and not the one key it reads, because the rule
// for a slot belongs to the person and grows with their file: a limit for each
// project is the next key that this count has to read.
func (s *Store) FreeSlots(cfg config.Config) (int, error) {
	return freeSlots(s.db, cfg)
}

// ClaimNext claims the first ticket of the queue whose project has room for
// it, and returns the ticket it claimed and the id of the run it wrote. The
// room, the read of the queue and the claim are one transaction, so two
// supervisors that start at the same time cannot take the same ticket: the
// second one reads the queue after the first one has committed, and finds the
// ticket in running. It gives ErrNoRoom when the queue holds nothing for it.
//
// cfg gives the limit of the person, and the claim counts the slots inside its
// own transaction: a supervisor that a trigger started for a slot that has
// since been taken finds none and stops.
//
// The ticket it takes is not always the first of the queue. Each project has
// the limit cfg.ProjectRuns of its own, and a ticket whose project is at that
// limit waits while a later ticket of a project with room starts.
//
// branch gives the name of the branch for the ticket, because only the
// transaction knows which ticket that is. It runs while the transaction holds
// the writer lock, so it must do no work of its own on the database.
func (s *Store) ClaimNext(cfg config.Config, branch func(Ticket) string) (Ticket, int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Ticket{}, 0, err
	}
	defer tx.Rollback()

	free, err := freeSlots(tx, cfg)
	if err != nil {
		return Ticket{}, 0, err
	}
	if free == 0 {
		return Ticket{}, 0, ErrNoRoom
	}
	id, err := nextWithRoom(tx, cfg)
	if err != nil {
		return Ticket{}, 0, err
	}

	t, err := ticket(tx, id)
	if err != nil {
		return Ticket{}, 0, err
	}
	t.Branch = branch(t)
	runID, err := claim(tx, t.ID, t.Branch)
	if err != nil {
		return Ticket{}, 0, err
	}
	if err := tx.Commit(); err != nil {
		return Ticket{}, 0, err
	}
	t.Status = Running
	return t, runID, nil
}

// nextWithRoom returns the id of the first ticket of the queue whose project
// has room for one more, and ErrNoRoom when no ticket of the queue has. The
// order is the order of the queue, so what it gives is the first ticket the
// person sees whose project is not full: a later ticket can start before an
// earlier one of a project that is at its limit.
//
// A project holds one of its own places for each of its tickets in running and
// in ready, which is what a slot of the whole queue counts as well. The limit
// is cfg.ProjectRuns and it is the same for every project, so the statement
// names no project and the config file names none either.
//
// A ticket that depends on a ticket that is not done is passed over the same
// way. Done satisfies a link and nothing else does, so a ticket does not start
// on work that a person has not accepted yet, and a link to a ticket that was
// cancelled holds the ticket back until the link goes.
func nextWithRoom(q querier, cfg config.Config) (int64, error) {
	var id int64
	err := q.QueryRow(`
		SELECT t.id FROM tickets AS t
		WHERE t.status = ? AND t.position IS NOT NULL
		  AND (SELECT COUNT(*) FROM tickets AS held
		       WHERE held.project_id = t.project_id AND held.status IN (?, ?)) < ?
		  AND NOT EXISTS (SELECT 1 FROM ticket_deps
		       JOIN tickets AS dependency ON dependency.id = ticket_deps.depends_on
		       WHERE ticket_deps.ticket_id = t.id AND dependency.status <> ?)
		ORDER BY t.position
		LIMIT 1`, Queued, Running, Ready, cfg.ProjectRuns(), Done).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNoRoom
	}
	return id, err
}

// claim is the writing of Claim without the transaction, so a caller that
// reads the queue in the same one can do all of it together.
func claim(tx *sql.Tx, id int64, branch string) (int64, error) {
	started := time.Now()
	if err := changeStatus(tx, id, Running, started); err != nil {
		return 0, err
	}
	if _, err := tx.Exec("UPDATE tickets SET branch = ? WHERE id = ?", branch, id); err != nil {
		return 0, err
	}
	result, err := tx.Exec(
		"INSERT INTO runs (ticket_id, pid, started_at) VALUES (?, ?, ?)",
		id, os.Getpid(), rfc3339(started),
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// ErrNoRun shows that a ticket has no run: no supervisor has claimed it, or no
// ticket holds the id.
var ErrNoRun = errors.New("the ticket has no run")

// Run is one row of the table runs. PID is the process id of the supervisor
// that claimed the ticket, and StartedAt is the time of the claim. PID is an
// int and not a sql.Null, because no process has the id 0. EndedAt is the
// zero time and ExitCode is not valid while the run is going; the exit code
// is a sql.Null because 0 is an exit code and not the absence of one.
type Run struct {
	ID        int64
	TicketID  int64
	PID       int
	StartedAt time.Time
	EndedAt   time.Time
	ExitCode  sql.Null[int]
}

// Run returns the last run of a ticket, which for a ticket in running is the
// run that holds it: a ticket that failed and went back to the queue has one
// row for each claim, and only the last one can still be alive. It gives
// ErrNoRun when the ticket has no run.
func (s *Store) Run(ticketID int64) (Run, error) {
	var r Run
	err := s.db.QueryRow(`
		SELECT id, ticket_id, COALESCE(pid, 0), started_at, ended_at, exit_code
		FROM runs
		WHERE ticket_id = ?
		ORDER BY id DESC
		LIMIT 1`, ticketID).Scan(
		&r.ID, &r.TicketID, &r.PID, timeColumn{&r.StartedAt}, timeColumn{&r.EndedAt}, &r.ExitCode)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, fmt.Errorf("%w: ticket %d", ErrNoRun, ticketID)
	}
	if err != nil {
		return Run{}, err
	}
	return r, nil
}

// EndRun writes the end time and the exit code on one run, which is the run of
// the supervisor that calls it. It takes the id that the claim gave that
// supervisor, and gives ErrNoRun when no run holds the id.
func (s *Store) EndRun(runID int64, exitCode int) error {
	result, err := s.db.Exec(
		"UPDATE runs SET ended_at = ?, exit_code = ? WHERE id = ?",
		rfc3339(time.Now()), exitCode, runID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: run %d", ErrNoRun, runID)
	}
	return nil
}

// Reconcile marks failed each ticket in running whose supervisor is gone, and
// gives its run the end time that the supervisor never wrote. It returns the
// number of tickets it marked.
//
// A supervisor can stop with no report, from a crash or from a restart of the
// computer, and a supervisor that is not there can write nothing, so only a
// later command can correct the ticket it left in running. Each command does
// this before its own work.
//
// running answers for one run, and the caller owns the rule: this package
// holds no way to ask the operating system about a program. The read and every
// write are one transaction, so a command that runs beside a supervisor sees
// the queue before the reconcile or after it, and never part way through.
func (s *Store) Reconcile(running func(Run) bool) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	runs, err := runningRuns(tx)
	if err != nil {
		return 0, err
	}
	marked := 0
	for _, r := range runs {
		if running(r) {
			continue
		}
		if err := failRun(tx, r.ID, r.TicketID, time.Now()); err != nil {
			return 0, err
		}
		marked++
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return marked, nil
}

// runningRuns returns the last run of each ticket in running, which is the run
// that holds the ticket. The rows are read into memory before the caller
// writes, because one transaction gives one connection and a write on it would
// wait for the rows to close.
func runningRuns(tx *sql.Tx) ([]Run, error) {
	rows, err := tx.Query(`
		SELECT r.id, r.ticket_id, COALESCE(r.pid, 0), r.started_at, r.ended_at, r.exit_code
		FROM runs r
		JOIN tickets t ON t.id = r.ticket_id
		WHERE t.status = ?
		AND r.id = (SELECT MAX(id) FROM runs WHERE ticket_id = t.id)
		ORDER BY r.ticket_id`, Running)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []Run
	for rows.Next() {
		var r Run
		if err := rows.Scan(&r.ID, &r.TicketID, &r.PID,
			timeColumn{&r.StartedAt}, timeColumn{&r.EndedAt}, &r.ExitCode); err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// FailUnfinished marks the ticket of a run failed when it is still running,
// and writes the end time on that run when the run has none. A ticket in any
// other state is left as it is, and that is not an error: dg finish made it
// ready, or a cancel made it cancelled, and both of those are a report. It
// gives ErrNoRun when no run holds the id.
//
// The supervisor calls it as its own run ends, with the id that its claim
// gave it. Only dg finish makes a ticket ready, so a ticket still in running
// at that moment is a run that stopped early, and a run that gave no report
// did not succeed. The reconcile answers for a supervisor that is gone, and
// this answers for one that is there.
func (s *Store) FailUnfinished(runID int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var ticketID int64
	var status TicketStatus
	err = tx.QueryRow(`
		SELECT t.id, t.status
		FROM runs r
		JOIN tickets t ON t.id = r.ticket_id
		WHERE r.id = ?`, runID).Scan(&ticketID, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: run %d", ErrNoRun, runID)
	}
	if err != nil {
		return err
	}
	if status != Running {
		return nil
	}
	if err := failRun(tx, runID, ticketID, time.Now()); err != nil {
		return err
	}
	return tx.Commit()
}

// endRunAt gives one run an end time, so that no run of a ticket that is not
// running is left open. It is the write behind failRun and Cancel, which reach
// a run that its own supervisor could not close: one that crashed, and one
// that a signal ended.
//
// An end time that is there stays. That time is the moment the run ended, from
// the supervisor that wrote it with the exit code or from an earlier
// reconcile, and a second one would move the end of the run to a moment after
// it.
//
// A run id of 0 reaches no row, which is how a caller says that it has no run
// to close.
//
// EndRun is not this: the supervisor writes its own end with the exit code,
// takes no time from its caller, and wants ErrNoRun for an id that no run has.
func endRunAt(tx *sql.Tx, runID int64, at time.Time) error {
	_, err := tx.Exec(
		"UPDATE runs SET ended_at = ? WHERE id = ? AND ended_at IS NULL",
		rfc3339(at), runID)
	return err
}

// failRun marks a ticket failed and gives one run of it an end time. Both ids
// are given because each caller holds both, and a run named by its id is the
// run the caller means: the last run of a ticket is not always the one that a
// supervisor holds.
func failRun(tx *sql.Tx, runID, ticketID int64, at time.Time) error {
	if err := changeStatus(tx, ticketID, Failed, at); err != nil {
		return err
	}
	return endRunAt(tx, runID, at)
}

// Cancel closes a ticket the person has stopped, and gives one run of it the
// end time when that run has none. It works from each state that is not the
// end, and gives ErrInvalidTicketStateChange from done and cancelled, so a
// ticket that is already closed does not reopen.
//
// A ticket in running has a supervisor, and the caller stops that supervisor
// before it calls this: the signal goes outside the transaction, because this
// one holds SQLite's writer lock and the wait between the two signals is
// seconds. A supervisor that a signal ended writes nothing, so the end time of
// its run comes from here.
//
// runID names that run, the way it does for the supervisor in FailUnfinished:
// it is the run the caller read and stopped, and not whichever run of the
// ticket is the last one now. The two are the same in an ordinary cancel and
// come apart in a race, where a ticket that failed between the stop and this
// call went back to the queue and a new supervisor claimed it; a query for the
// last run would then close the run of that supervisor, which is still going.
//
// runID is 0 for a ticket with no run to close, which is every state but
// running: a ticket that never ran has no row, and a run that ended wrote its
// own end time. No run has the id 0, because the column is INTEGER PRIMARY KEY.
func (s *Store) Cancel(id, runID int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	at := time.Now()
	if err := changeStatus(tx, id, Cancelled, at); err != nil {
		return err
	}
	if err := endRunAt(tx, runID, at); err != nil {
		return err
	}
	return tx.Commit()
}

// FinishTicket completes a Running ticket. It records the commit of the run, and
// the change into ready holds the time that the run stopped, which DONE is
// ordered by and dg show writes.
//
// If the ticket is not Running, it returns ErrInvalidTicketStateChange.
func (s *Store) FinishTicket(id int64, commit string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := changeStatus(tx, id, Ready, time.Now()); err != nil {
		return err
	}
	if _, err := tx.Exec(
		"UPDATE tickets SET commit_id = ? WHERE id = ?", commit, id,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// ChangeStatusWith changes a status and runs work before the transaction
// commits, so work that fails leaves the status as it was. Use it when
// something outside the database must not happen unless the change holds:
// dg accept removes a worktree, and a ticket it failed to remove must stay
// ready rather than close with the work still on disk.
//
// This holds SQLite's writer lock while work runs, which Claim is careful not
// to do. Removing a worktree takes a few milliseconds, where creating one
// checks out every file, so the wait it puts on another command is not the same
// wait.
func (s *Store) ChangeStatusWith(id int64, status TicketStatus, work func() error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := changeStatus(tx, id, status, time.Now()); err != nil {
		return err
	}
	if err := work(); err != nil {
		return err
	}
	return tx.Commit()
}

// SetSession records the session a run reported, so a person can open the
// conversation later. The supervisor writes it after the run ends, because
// every agent reports its own id and none takes one from delegator.
func (s *Store) SetSession(id int64, session string) error {
	result, err := s.db.Exec("UPDATE tickets SET session = ? WHERE id = ?", session, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: %d", ErrNoTicket, id)
	}
	return nil
}

// SetTitle records a new title for a ticket. The title is a column and not the
// first line of the prose, so a person who corrects it needs a command that
// writes the column, and dg edit is that command.
func (s *Store) SetTitle(id int64, title string) error {
	result, err := s.db.Exec("UPDATE tickets SET title = ? WHERE id = ?", title, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: %d", ErrNoTicket, id)
	}
	return nil
}

// IsQueueRunning returns if the queue is actively running. A paused queue
// prevents tickets from automatically starting.
func (s *Store) IsQueueRunning() (bool, error) {
	var running bool
	err := s.db.QueryRow("SELECT running FROM queue_state WHERE id = 1").Scan(&running)
	if err != nil {
		return false, err
	}
	return running, nil
}

// setQueueRunning adjusts the running status of the queue. When running, tickets
// will be worked on automatically. When paused, running tickets will complete
// and no new tickets will be worked on automatically.
func (s *Store) setQueueRunning(running bool) error {
	result, err := s.db.Exec("UPDATE queue_state SET running = ? WHERE id = 1", running)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("the queue_state row is missing")
	}
	return nil
}

// PauseQueue pauses the queue.
func (s *Store) PauseQueue() error {
	return s.setQueueRunning(false)
}

// ResumeQueue starts the queue.
func (s *Store) ResumeQueue() error {
	return s.setQueueRunning(true)
}
