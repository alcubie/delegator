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

// ErrNotInTheQueue shows that a ticket is not one that the queue holds. The id
// can belong to no ticket, or to a ticket that has a status other than queued.
var ErrNotInTheQueue = errors.New("the ticket is not in the queue")

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
		rfc3339(time.Now()),
	}

	return s.create(query, args...)
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

// MoveTicket moves one ticket in the queue, in the direction of move. The read
// of the order and the write of each new position are in one transaction, so
// the order that moves is the order that the queue has.
func (s *Store) MoveTicket(id int64, move Move) error {
	return s.move(id, func(ids []int64, from int) ([]int64, error) {
		return reorder(ids, from, move), nil
	})
}

// MoveTicketBefore puts one ticket where target is, and target and each ticket
// below it go down one place. A person who moves a ticket into the middle of
// the queue therefore writes one command, and not one dg move up for each place.
//
// A target that is the ticket itself changes nothing, because a ticket is
// already where it is.
func (s *Store) MoveTicketBefore(id, target int64) error {
	return s.move(id, func(ids []int64, from int) ([]int64, error) {
		if !slices.Contains(ids, target) {
			return nil, fmt.Errorf("%w: ticket %d", ErrNotInTheQueue, target)
		}
		return reorderBefore(ids, from, target), nil
	})
}

// move reads the order of the queue, gives it to order, and writes what comes
// back. The read and the write are below one transaction, so the order that
// moves is the order that the queue has.
func (s *Store) move(id int64, order func(ids []int64, from int) ([]int64, error)) error {
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

	moved, err := order(ids, from)
	if err != nil {
		return err
	}
	if err := setPositions(tx, moved); err != nil {
		return err
	}
	return tx.Commit()
}

// queuedIDs returns the id of each ticket of the queue, in the order of
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

// setPositions writes the order of the queue. The first id of ids takes
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
	ID       int64
	Project  string
	Title    string
	Status   TicketStatus
	Position int

	// Completed is the time that the ticket became ready. A ticket that never
	// became ready holds the zero time.
	Completed time.Time

	// Started is the time that the last run of the ticket began. For a ticket
	// in running that is the run that holds it, and the inbox takes the
	// duration of the run from it. A ticket that no supervisor has claimed
	// holds the zero time.
	Started time.Time
}

// OpenTickets returns each ticket that is still open: the ones that wait, the
// one that runs, and the ones that are complete and wait for the person. A
// ticket that is done or cancelled is closed, and DoneTickets returns the ones
// of those that the inbox still shows.
//
// One query returns the tickets of each project, because the inbox is one list
// for all projects.
func (s *Store) OpenTickets() ([]OpenTicket, error) {
	return s.inboxTickets(inboxTicketQuery+`
		WHERE tickets.status IN (?, ?, ?)
		ORDER BY tickets.id`, Queued, Running, Ready)
}

// DoneTickets returns each ticket that dg accept closed and that finished at
// or after since. The inbox holds them for a while after they close, so a
// person who accepted a ticket can still read what it was.
//
// A ticket that is cancelled is not one of these. It was never finished, and
// its completed column is the time of a run that the person threw away.
//
// A done ticket with no completed column is not one either. dg finish writes
// that column and only a ticket that dg finish made ready can become done, so
// the column is empty for a row that a version before it left behind.
func (s *Store) DoneTickets(since time.Time) ([]OpenTicket, error) {
	// completed holds the form of rfc3339 in UTC, which is one width and one
	// zone for every row, so a comparison of the text is a comparison of the
	// times.
	return s.inboxTickets(inboxTicketQuery+`
		WHERE tickets.status = ? AND tickets.completed >= ?
		ORDER BY tickets.id`, Done, rfc3339(ceilSecond(since)))
}

// ceilSecond rounds a time up to the next whole second, and leaves a time that
// is already whole as it is.
//
// The column completed names a second and no part of one, so the window rounds
// the same way: a ticket is in it only when the whole second its column names
// is. Without this a window of no length would still hold each ticket that
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
	       COALESCE(tickets.position, 0), tickets.completed,
	       (SELECT started_at FROM runs
	        WHERE runs.ticket_id = tickets.id ORDER BY runs.id DESC LIMIT 1)
	FROM tickets
	JOIN projects ON projects.id = tickets.project_id
	`

// inboxTickets runs a query that inboxTicketQuery starts and reads each row of
// it into an OpenTicket. Store.Ticket does not use it: that one reads the
// branch, the session and the commit as well, which no row of the inbox shows.
func (s *Store) inboxTickets(query string, args ...any) ([]OpenTicket, error) {
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
			timeColumn{&t.Completed}, timeColumn{&t.Started}); err != nil {
			return nil, err
		}
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
	ID        int64
	Project   Project
	Title     string
	Status    TicketStatus
	Position  int
	Branch    string
	Session   string
	Commit    string
	Created   time.Time
	Completed time.Time
}

// Ticket returns one ticket. It gives ErrNoTicket if the id holds none.
func (s *Store) Ticket(id int64) (Ticket, error) {
	var t Ticket
	err := s.db.QueryRow(`
		SELECT tickets.id, projects.id, projects.path, projects.default_branch,
		       tickets.title, tickets.status,
		       COALESCE(tickets.position, 0), COALESCE(tickets.branch, ''),
		       COALESCE(tickets.session, ''), COALESCE(tickets.commit_id, ''),
		       tickets.created, tickets.completed
		FROM tickets
		JOIN projects ON projects.id = tickets.project_id
		WHERE tickets.id = ?`, id).Scan(
		&t.ID, &t.Project.ID, &t.Project.Path, &t.Project.DefaultBranch,
		&t.Title, &t.Status, &t.Position, &t.Branch,
		&t.Session, &t.Commit, timeColumn{&t.Created}, timeColumn{&t.Completed})
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
// Position follows status: entering queued appends to the end, leaving it
// clears the position, so the CHECK constraint holds either way.
//
// The read and the write share one transaction, which is BEGIN IMMEDIATE, so
// nothing can change the status in between.
func (s *Store) ChangeStatus(id int64, status TicketStatus) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := changeStatus(tx, id, status); err != nil {
		return err
	}
	return tx.Commit()
}

// changeStatus is ChangeStatus without the transaction, so a caller that writes
// more than the status can do all of it in one.
func changeStatus(tx *sql.Tx, id int64, status TicketStatus) error {
	var from TicketStatus
	err := tx.QueryRow("SELECT status FROM tickets WHERE id = ?", id).Scan(&from)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %d", ErrNoTicket, id)
	}
	if err != nil {
		return err
	}
	if !canChange(from, status) {
		return fmt.Errorf("%w: %s to %s", ErrInvalidTicketStateChange, from, status)
	}

	update := "UPDATE tickets SET status = ?, position = NULL WHERE id = ?"
	if status == Queued {
		update = `UPDATE tickets
			SET status = ?, position = (SELECT COALESCE(MAX(position), 0) + 1 FROM tickets)
			WHERE id = ?
		`
	}
	_, err = tx.Exec(update, status, id)
	return err
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

	if err := changeStatus(tx, id, Running); err != nil {
		return 0, err
	}
	if _, err := tx.Exec("UPDATE tickets SET branch = ? WHERE id = ?", branch, id); err != nil {
		return 0, err
	}
	result, err := tx.Exec(
		"INSERT INTO runs (ticket_id, pid, started_at) VALUES (?, ?, ?)",
		id, os.Getpid(), rfc3339(time.Now()),
	)
	if err != nil {
		return 0, err
	}
	runID, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return runID, nil
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

// EndRun writes the end time and the exit code on the last run of a ticket,
// which is the run of the supervisor that calls it. It gives ErrNoRun when the
// ticket has no run.
func (s *Store) EndRun(ticketID int64, exitCode int) error {
	result, err := s.db.Exec(`
		UPDATE runs SET ended_at = ?, exit_code = ?
		WHERE id = (SELECT id FROM runs WHERE ticket_id = ? ORDER BY id DESC LIMIT 1)`,
		rfc3339(time.Now()), exitCode, ticketID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: ticket %d", ErrNoRun, ticketID)
	}
	return nil
}

// FailUnfinished marks a ticket failed when it is still running, and writes
// the end time on its run when the run has none. A ticket in any other state
// is left as it is, and that is not an error: dg finish made it ready, or a
// cancel made it cancelled, and both of those are a report.
//
// The supervisor calls it as its own run ends. Only dg finish makes a ticket
// ready, so a ticket still in running at that moment is a run that stopped
// early, and a run that gave no report did not succeed. The reconcile answers
// for a supervisor that is gone, and this answers for one that is there.
func (s *Store) FailUnfinished(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var status TicketStatus
	err = tx.QueryRow("SELECT status FROM tickets WHERE id = ?", id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %d", ErrNoTicket, id)
	}
	if err != nil {
		return err
	}
	if status != Running {
		return nil
	}
	if err := failRun(tx, id, time.Now()); err != nil {
		return err
	}
	return tx.Commit()
}

// failRun marks a ticket failed and gives its last run an end time. An end
// time that is there stays: the supervisor wrote it with the exit code before
// it stopped, and that is the moment the run ended.
func failRun(tx *sql.Tx, ticketID int64, at time.Time) error {
	if err := changeStatus(tx, ticketID, Failed); err != nil {
		return err
	}
	_, err := tx.Exec(`
		UPDATE runs SET ended_at = ?
		WHERE id = (SELECT id FROM runs WHERE ticket_id = ? ORDER BY id DESC LIMIT 1)
		AND ended_at IS NULL`,
		rfc3339(at), ticketID)
	return err
}

// FinishTicket completes a Running ticket. It records the commit of the run and
// the time the run stopped, which orders the ready tickets in the inbox.
//
// If the ticket is not Running, it returns ErrInvalidTicketStateChange.
func (s *Store) FinishTicket(id int64, commit string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := changeStatus(tx, id, Ready); err != nil {
		return err
	}
	if _, err := tx.Exec(
		"UPDATE tickets SET commit_id = ?, completed = ? WHERE id = ?",
		commit, rfc3339(time.Now()), id,
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

	if err := changeStatus(tx, id, status); err != nil {
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
