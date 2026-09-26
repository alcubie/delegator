// Package store persists delegator state in SQLite. Ticket descriptions live
// in separate files so users can edit them directly.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/alcubie/delegator/internal/config"

	// Register the SQLite driver with database/sql.
	_ "modernc.org/sqlite"
)

// dbFile is the database filename. Changing it requires migrating existing
// installations.
const dbFile = "delegator.db"

// dirPerm restricts the data directory and its subdirectories to the owner,
// protecting ticket contents and run artifacts.
const dirPerm = 0o700

// filePerm restricts the database to its owner. Open creates the file before
// SQLite so the WAL inherits these permissions; chmod after opening could
// leave the WAL readable by other users.
const filePerm = 0o600

// dataDirs lists the data root and subdirectories for ticket descriptions,
// worktrees, and run logs.
var dataDirs = []string{"", "tickets", "worktrees", "runs"}

// busyTimeout is the SQLite writer-lock timeout in milliseconds. Concurrent
// commands and supervisors should wait for brief writes instead of failing
// immediately.
const busyTimeout = 5000

// dsn builds the SQLite connection URL. Settings belong in the DSN so they
// apply to every pooled connection. URL escaping preserves paths containing ?
// or #.
//
// _txlock=immediate acquires the writer lock at BEGIN. Upgrading a read
// transaction to a write can fail with SQLITE_BUSY without waiting for
// busy_timeout. Read-only transactions still use a plain BEGIN.
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

// rfc3339 formats timestamps as UTC RFC 3339 text with second precision, so
// lexical and chronological ordering agree.
func rfc3339(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// timeColumn scans a stored timestamp into time.Time, mapping NULL to the
// zero time.
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

// querier lets read helpers accept either *sql.DB or *sql.Tx.
type querier interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// Store holds the database connection pool. Each command opens and closes its
// own Store.
type Store struct {
	db  *sql.DB
	dir string
}

// DataDir returns the directory the store was opened in. The database, the
// worktrees, the project caches, the runs, and the ticket prose all live below
// it.
func (s *Store) DataDir() string {
	return s.dir
}

// TicketStatus is the state of a ticket.
type TicketStatus string

// Ticket states. Done and Cancelled are terminal. Only dg finish marks work
// Ready; an agent exiting without a report leaves the ticket Failed. See
// nextStates for allowed transitions.
const (
	Queued    TicketStatus = "queued"
	Running   TicketStatus = "running"
	Ready     TicketStatus = "ready"
	Failed    TicketStatus = "failed"
	Done      TicketStatus = "done"
	Cancelled TicketStatus = "cancelled"
)

// nextStates defines the allowed ticket state transitions.
var nextStates = map[TicketStatus][]TicketStatus{
	Queued:    {Running, Cancelled},
	Running:   {Ready, Failed, Cancelled},
	Ready:     {Done, Cancelled},
	Failed:    {Running, Ready, Cancelled},
	Done:      nil,
	Cancelled: nil,
}

// canChange reports whether one status can change to another.
func canChange(from, to TicketStatus) bool {
	return slices.Contains(nextStates[from], to)
}

// ErrInvalidTicketStateChange shows that an invalid state change was attempted and blocked.
var ErrInvalidTicketStateChange = errors.New("invalid ticket state change")

// ErrNotMovable means the ticket is missing or is neither queued nor ready.
var ErrNotMovable = errors.New("only a queued ticket and a ready ticket can move")

// ErrNotInTheSameList means a move target is outside the ticket's list.
// Queued and ready tickets have separate orderings.
var ErrNotInTheSameList = errors.New("a ticket moves inside its own list")

// Open returns the database below dataDir. It makes the data directory, the
// directories below it, and the database, if they are not present.
func Open(dataDir string) (*Store, error) {
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

// With opens the store, calls fn, and closes the store. It returns fn's error
// if present, otherwise any error from Close.
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

// Close closes the database connection pool.
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

// ProjectID returns the project ID for path, creating the project if needed.
// The first ticket sets its default branch; later tickets preserve that
// choice.
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

// AddTicket creates a queued ticket with dependencies on dependsOn. A missing
// dependency returns ErrNoTicket without changing the database.
//
// The ticket, dependencies, and initial history entry are written atomically
// so a supervisor cannot claim the ticket before its dependencies exist. The
// history entry records its creation time.
func (s *Store) AddTicket(projectID int64, title string, dependsOn ...int64) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// Assign the next queue position in the INSERT to avoid a race
	// between reading and writing it.
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

// QueuedTicket holds the fields needed to display a queued ticket.
type QueuedTicket struct {
	ID    int64
	Title string
}

// ListQueue returns queued tickets in position order. Membership requires
// both queued status and a non-NULL position.
func (s *Store) ListQueue() ([]QueuedTicket, error) {
	return listQueue(s.db)
}

// listQueue implements ListQueue for either a database or transaction.
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
	return queue, rows.Err()
}

// list pairs a reorderable inbox status with its position column, keeping
// queued and ready ordering separate.
type list struct {
	status TicketStatus
	column string
}

// lists holds each list that a move can order.
var lists = []list{
	{Queued, "position"},
	{Ready, "ready_position"},
}

// MoveTicket reorders a ticket within its list. Reading and updating
// positions share one transaction.
func (s *Store) MoveTicket(id int64, move Move) error {
	return s.move(id, func(_ list, ids []int64, from int) ([]int64, error) {
		return reorder(ids, from, move), nil
	})
}

// MoveTicketBefore places a ticket immediately before target, shifting later
// entries down. Moving a ticket before itself does nothing.
func (s *Store) MoveTicketBefore(id, target int64) error {
	return s.move(id, func(where list, ids []int64, from int) ([]int64, error) {
		if !slices.Contains(ids, target) {
			return nil, fmt.Errorf("%w: ticket %d is not %s", ErrNotInTheSameList, target, where.status)
		}
		return reorderBefore(ids, from, target), nil
	})
}

// move applies order to the ticket's list and saves the result in one
// transaction.
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

// listOf returns the ticket's reorderable list, or ErrNotMovable if the
// ticket is missing or belongs to neither list.
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

// listIDs returns ticket IDs in list order, matching the order shown in the
// inbox.
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
	// Move positions below zero before assigning final values to avoid
	// unique-index collisions. NULL cannot be used temporarily because
	// SQLite checks the required position immediately.
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

// OpenTicket holds a ticket and its project path for inbox display. NULL
// columns become zero values only where zero is not a valid stored value;
// otherwise a nullable type is needed.
type OpenTicket struct {
	ID      int64
	Project string
	Title   string
	Status  TicketStatus

	// Position is the ticket's queue or ready-list position, or zero for
	// other statuses.
	Position int

	// Created is the timestamp of the ticket's initial history entry.
	Created time.Time

	// Accepted is the transition time into Done, or zero if never
	// accepted. It determines the order and cutoff for DONE.
	Accepted time.Time

	// Started is the latest run's start time, or zero if the ticket has
	// never run. The inbox uses it to calculate elapsed run time.
	Started time.Time

	// Changed is the latest status change time, or zero if no history
	// exists. It determines the order of FAILED.
	Changed time.Time

	// DependsOn lists unfinished dependency IDs in ascending order.
	// Completed dependencies are omitted so the inbox shows only
	// blockers.
	DependsOn []int64
}

// OpenTickets returns queued, running, ready, and failed tickets across all
// projects. DoneTickets supplies recently accepted tickets separately.
func (s *Store) OpenTickets() ([]OpenTicket, error) {
	return s.inboxTickets(inboxTicketQuery+`
		WHERE tickets.status IN (?, ?, ?, ?)
		ORDER BY tickets.id`, Queued, Running, Ready, Failed)
}

// AllTickets returns tickets in ID order, including closed tickets. An empty
// project selects all projects; otherwise project is a repository path.
// Unlike the inbox, dg list shows this creation order.
func (s *Store) AllTickets(project string) ([]OpenTicket, error) {
	if project == "" {
		return s.inboxTickets(inboxTicketQuery + `ORDER BY tickets.id`)
	}
	return s.inboxTickets(inboxTicketQuery+`
		WHERE projects.path = ?
		ORDER BY tickets.id`, project)
}

// DoneTickets returns tickets accepted at or after since. The window starts
// at acceptance, even if the work finished much earlier. Cancelled tickets
// and legacy done tickets without acceptance history are excluded.
func (s *Store) DoneTickets(since time.Time) ([]OpenTicket, error) {
	// UTC timestamps with fixed precision sort chronologically as text.
	return s.inboxTickets(inboxTicketQuery+`
		WHERE tickets.status = ? AND `+acceptedTime+` >= ?
		ORDER BY tickets.id`, Done, rfc3339(ceilSecond(since)))
}

// ceilSecond rounds up to whole-second precision. Rounding the cutoff down
// would include timestamps before since, potentially showing tickets even
// with a zero-length DONE window.
func ceilSecond(t time.Time) time.Time {
	whole := t.Truncate(time.Second)
	if whole.Equal(t) {
		return t
	}
	return whole.Add(time.Second)
}

// inboxTicketQuery selects OpenTicket columns; callers append WHERE and ORDER
// BY clauses. A subquery selects the latest run, avoiding duplicate tickets
// when several runs exist.
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

// inboxTickets scans an inboxTicketQuery result into OpenTickets.
// Store.Ticket separately reads details such as branch, session, and commit.
func (s *Store) inboxTickets(query string, args ...any) ([]OpenTicket, error) {
	// Load dependencies in one query to avoid a separate lookup per
	// ticket.
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

	// Created is the timestamp of the ticket's initial history entry.
	Created time.Time

	// Accepted is the acceptance time, or zero if never accepted.
	Accepted time.Time

	// Changed is the latest status change time.
	Changed time.Time
}

// Ticket returns one ticket. It gives ErrNoTicket if the id holds none.
func (s *Store) Ticket(id int64) (Ticket, error) {
	return ticket(s.db, id)
}

// ticketExists returns ErrNoTicket if id is missing, without loading ticket
// details.
func ticketExists(q querier, id int64) error {
	var found int
	err := q.QueryRow("SELECT 1 FROM tickets WHERE id = ?", id).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %d", ErrNoTicket, id)
	}
	return err
}

// statusOf returns the ticket status or ErrNoTicket. Callers that act on the
// status must read and write within the same transaction.
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

// ChangeStatus applies an allowed transition or returns
// ErrInvalidTicketStateChange. A missing ticket returns ErrNoTicket.
//
// Entering queued or ready appends the ticket to that list; leaving clears
// its position. The status read, update, and history entry share an immediate
// transaction to prevent concurrent changes.
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

// changeStatus applies a transition within the caller's transaction and
// records its history at the supplied time.
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

// Claim atomically marks a ticket running, records its branch, and
// creates a run with the supervisor PID and claim time. A competing claim
// returns ErrInvalidTicketStateChange.
//
// The returned run ID identifies this supervisor's run. Use it for later
// updates rather than querying the latest run, which may belong to a
// subsequent restart.
//
// Keep worktree creation outside the claim transaction. Git operations
// while holding SQLite's writer lock would delay other commands.
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

// ErrNoRoom means no ticket can be claimed: the queue is paused or empty,
// capacity is full, or dependencies block the remaining work. Supervisors
// treat this as a normal exit.
var ErrNoRoom = errors.New("no ticket in the queue has room to run")

// freeSlots returns global capacity minus running and ready tickets, clamped
// to zero. A paused queue has no free slots. This counts capacity, not queued
// work.
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

// ClaimableCount returns how many queued tickets can start now, respecting
// dependencies, per-project capacity, global capacity, and pause state. It
// uses the same eligibility rule as ClaimNext.
//
// Cap each project's contribution at its remaining capacity. Overcounting can
// cause an endless loop of supervisors finding no work and triggering
// replacements.
func (s *Store) ClaimableCount(cfg config.Config) (int, error) {
	free, err := freeSlots(s.db, cfg)
	if err != nil {
		return 0, err
	}
	args := append([]any{cfg.ProjectRuns(), Running, Ready}, claimableArgs(cfg)...)
	var claimable int
	err = s.db.QueryRow(`
		SELECT COALESCE(SUM(MIN(waiting, room)), 0) FROM (
			SELECT COUNT(*) AS waiting, ? - `+placesHeld+` AS room
			FROM tickets AS t
			WHERE `+claimableRule+`
			GROUP BY t.project_id)`, args...).Scan(&claimable)
	if err != nil {
		return 0, err
	}
	return min(free, claimable), nil
}

// ClaimNext claims the first eligible queued ticket and returns it with the
// new run ID. Capacity checks, selection, and claim share one transaction,
// preventing duplicate claims or exceeding cfg limits. It returns ErrNoRoom
// when nothing can start.
//
// Tickets blocked by dependencies or a full project are skipped. The branch
// callback names the selected ticket's branch while the writer lock is held,
// so it must not access the database.
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

// placesHeld is a SQL fragment counting running and ready tickets in the
// project of outer ticket t. It takes those two statuses as parameters.
const placesHeld = `(SELECT COUNT(*) FROM tickets AS held
		       WHERE held.project_id = t.project_id AND held.status IN (?, ?))`

// claimableRule is the shared SQL eligibility predicate for claiming tickets
// and counting supervisors to start. It refers to outer ticket t and takes
// claimableArgs parameters.
//
// A ticket must be queued, have project capacity, and have all dependencies
// Done. Cancelled dependencies remain blockers until removed.
const claimableRule = `t.status = ? AND t.position IS NOT NULL
		  AND ` + placesHeld + ` < ?
		  AND NOT EXISTS (SELECT 1 FROM ticket_deps
		       JOIN tickets AS dependency ON dependency.id = ticket_deps.depends_on
		       WHERE ticket_deps.ticket_id = t.id AND dependency.status <> ?)`

// claimableArgs gives the parameters of claimableRule in the order it takes
// them.
func claimableArgs(cfg config.Config) []any {
	return []any{Queued, Running, Ready, cfg.ProjectRuns(), Done}
}

// nextWithRoom returns the first queued ID satisfying claimableRule, or
// ErrNoRoom. Blocked tickets may be skipped in favor of later eligible work.
func nextWithRoom(q querier, cfg config.Config) (int64, error) {
	var id int64
	err := q.QueryRow(`
		SELECT t.id FROM tickets AS t
		WHERE `+claimableRule+`
		ORDER BY t.position
		LIMIT 1`, claimableArgs(cfg)...).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNoRoom
	}
	return id, err
}

// claim performs Claim's writes within the caller's transaction.
func claim(tx *sql.Tx, id int64, branch string) (int64, error) {
	started := time.Now()
	if err := changeStatus(tx, id, Running, started); err != nil {
		return 0, err
	}
	if _, err := tx.Exec("UPDATE tickets SET branch = ? WHERE id = ?", branch, id); err != nil {
		return 0, err
	}
	return startRun(tx, id, started)
}

// startRun records a new run after its ticket becomes running. Claim and
// Restart share this process and start-time bookkeeping.
func startRun(tx *sql.Tx, ticketID int64, started time.Time) (int64, error) {
	result, err := tx.Exec(
		"INSERT INTO runs (ticket_id, pid, started_at) VALUES (?, ?, ?)",
		ticketID, os.Getpid(), rfc3339(started),
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// AgentID returns the id of a registry entry. The registry is authoritative:
// a run cannot silently create an agent with no launch command.
func (s *Store) AgentID(name string) (int64, error) {
	var id int64
	if err := s.db.QueryRow("SELECT id FROM agents WHERE name = ?", name).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("%w: %s", ErrInvalidAgent, name)
		}
		return 0, err
	}
	return id, nil
}

// Agent is one runnable ACP agent in the registry.
type Agent struct {
	ID          int64
	Name        string
	Argv        []string
	Resume      []string
	InstallHint string
}

var ErrInvalidAgent = errors.New("invalid agent")

func validateAgent(a Agent) error {
	if strings.TrimSpace(a.Name) == "" || len(a.Argv) == 0 {
		return fmt.Errorf("%w: name and a non-empty argv are required", ErrInvalidAgent)
	}
	for _, arg := range a.Argv {
		if strings.TrimSpace(arg) == "" {
			return fmt.Errorf("%w: argv cannot contain an empty argument", ErrInvalidAgent)
		}
	}
	for _, arg := range a.Resume {
		if strings.TrimSpace(arg) == "" {
			return fmt.Errorf("%w: resume argv cannot contain an empty argument", ErrInvalidAgent)
		}
	}
	return nil
}

func encodeArgv(argv []string) (string, error) { b, err := json.Marshal(argv); return string(b), err }
func decodeArgv(raw string) ([]string, error) {
	var v []string
	return v, json.Unmarshal([]byte(raw), &v)
}

// Agents returns the registry in name order.
func (s *Store) Agents() ([]Agent, error) {
	rows, err := s.db.Query("SELECT id, name, argv, resume_argv, COALESCE(install_hint, '') FROM agents ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Agent
	for rows.Next() {
		var a Agent
		var argv, resume string
		if err := rows.Scan(&a.ID, &a.Name, &argv, &resume, &a.InstallHint); err != nil {
			return nil, err
		}
		var err error
		if a.Argv, err = decodeArgv(argv); err != nil {
			return nil, err
		}
		if a.Resume, err = decodeArgv(resume); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Agent returns the named registry entry.
func (s *Store) Agent(name string) (Agent, error) {
	var a Agent
	var argv, resume string
	err := s.db.QueryRow("SELECT id, name, argv, resume_argv, COALESCE(install_hint, '') FROM agents WHERE name = ?", name).
		Scan(&a.ID, &a.Name, &argv, &resume, &a.InstallHint)
	if errors.Is(err, sql.ErrNoRows) {
		return Agent{}, fmt.Errorf("%w: %s", ErrInvalidAgent, name)
	}
	if err != nil {
		return Agent{}, err
	}
	var e error
	if a.Argv, e = decodeArgv(argv); e != nil {
		return Agent{}, e
	}
	if a.Resume, e = decodeArgv(resume); e != nil {
		return Agent{}, e
	}
	return a, nil
}

// SaveAgent adds or replaces an agent registry entry.
func (s *Store) SaveAgent(a Agent) error {
	if err := validateAgent(a); err != nil {
		return err
	}
	argv, err := encodeArgv(a.Argv)
	if err != nil {
		return err
	}
	resume, err := encodeArgv(a.Resume)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO agents (name, argv, resume_argv, install_hint) VALUES (?, ?, ?, ?)
ON CONFLICT(name) DO UPDATE SET argv=excluded.argv, resume_argv=excluded.resume_argv, install_hint=excluded.install_hint`, a.Name, argv, resume, a.InstallHint)
	return err
}

// DefaultAgent returns the name selected for new runs. An empty name means
// that the person has not selected an agent yet.
func (s *Store) DefaultAgent() (string, error) {
	var name string
	err := s.db.QueryRow(`SELECT COALESCE(agents.name, '') FROM settings
LEFT JOIN agents ON agents.id = settings.default_agent_id WHERE settings.id = 1`).Scan(&name)
	return name, err
}

// SetDefaultAgent selects an existing registry entry for new runs.
func (s *Store) SetDefaultAgent(name string) error {
	result, err := s.db.Exec(`UPDATE settings SET default_agent_id = agents.id
FROM agents WHERE settings.id = 1 AND agents.name = ?`, name)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("%w: %s", ErrInvalidAgent, name)
	}
	return nil
}

// Settings returns one typed snapshot of all instance settings.
func (s *Store) Settings() (config.Config, error) {
	var cfg config.Config
	err := s.db.QueryRow(`SELECT settings.runs, settings.timeout_minutes,
settings.done_hours, settings.max_runs_per_project, COALESCE(agents.name, '')
FROM settings LEFT JOIN agents ON agents.id = settings.default_agent_id
WHERE settings.id = 1`).Scan(&cfg.Runs, &cfg.TimeoutMinutes, &cfg.DoneHours,
		&cfg.MaxRunsPerProject, &cfg.DefaultAgent)
	if err != nil {
		return config.Config{}, err
	}
	return cfg, nil
}

// integerSettingColumns is both the allowlist for a configurable integer and
// the SQL identifier it names. The query below uses only values from this map;
// a setting name supplied by a person is never concatenated into SQL.
var integerSettingColumns = map[string]string{
	"runs":                 "runs",
	"timeout_minutes":      "timeout_minutes",
	"done_hours":           "done_hours",
	"max_runs_per_project": "max_runs_per_project",
}

// SetSetting parses and atomically updates one named setting. default_agent
// is special because its public value is a registry name while SQLite stores
// the referenced agent id.
func (s *Store) SetSetting(name, value string) error {
	if name == "default_agent" {
		return s.SetDefaultAgent(value)
	}
	column, ok := integerSettingColumns[name]
	if !ok {
		return fmt.Errorf("unknown setting %q", name)
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("setting %s must be an integer", name)
	}
	result, err := s.db.Exec("UPDATE settings SET "+column+" = ? WHERE id = 1", n)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return errors.New("settings: instance settings row is missing")
	}
	return nil
}

// SetRunAgent records the agent one run started.
func (s *Store) SetRunAgent(runID, agentID int64) error {
	result, err := s.db.Exec("UPDATE runs SET agent_id = ? WHERE id = ?", agentID, runID)
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

// ErrNoRun means the requested run does not exist, or the requested ticket
// has never run.
var ErrNoRun = errors.New("the ticket has no run")

// Run records a supervisor PID and claim time. EndedAt is zero until the run
// ends. ExitCode is nullable because zero is a valid exit code, and a crashed
// supervisor may never report one.
type Run struct {
	ID        int64
	TicketID  int64
	AgentID   int64
	Agent     string
	PID       int
	StartedAt time.Time
	EndedAt   time.Time
	ExitCode  sql.Null[int]
}

// Run returns the ticket's latest run, or ErrNoRun if it has none.
func (s *Store) Run(ticketID int64) (Run, error) {
	var r Run
	err := s.db.QueryRow(`
		SELECT runs.id, runs.ticket_id, COALESCE(runs.agent_id, 0), COALESCE(agents.name, ''),
		       COALESCE(runs.pid, 0), runs.started_at, runs.ended_at, runs.exit_code
		FROM runs LEFT JOIN agents ON agents.id = runs.agent_id
		WHERE runs.ticket_id = ?
		ORDER BY runs.id DESC
		LIMIT 1`, ticketID).Scan(
		&r.ID, &r.TicketID, &r.AgentID, &r.Agent, &r.PID,
		timeColumn{&r.StartedAt}, timeColumn{&r.EndedAt}, &r.ExitCode)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, fmt.Errorf("%w: ticket %d", ErrNoRun, ticketID)
	}
	if err != nil {
		return Run{}, err
	}
	return r, nil
}

// EndRun records the end time and exit code for the supervisor's claimed run
// ID. It returns ErrNoRun if that run does not exist.
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

// Reconcile marks running tickets failed when their supervisors are gone and
// fills in missing run end times. It returns the number of tickets changed.
//
// Commands call this to recover from supervisor crashes or machine restarts.
// The caller supplies the process-liveness check; all database reads and
// writes share one transaction.
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

// runningRuns returns the latest run for each running ticket. Read and close
// the rows before writing through the transaction's single connection.
func runningRuns(tx *sql.Tx) ([]Run, error) {
	rows, err := tx.Query(`
		SELECT r.id, r.ticket_id, COALESCE(r.agent_id, 0), COALESCE(a.name, ''),
		       COALESCE(r.pid, 0), r.started_at, r.ended_at, r.exit_code
		FROM runs r
		JOIN tickets t ON t.id = r.ticket_id
		LEFT JOIN agents a ON a.id = r.agent_id
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
		if err := rows.Scan(&r.ID, &r.TicketID, &r.AgentID, &r.Agent, &r.PID,
			timeColumn{&r.StartedAt}, timeColumn{&r.EndedAt}, &r.ExitCode); err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// FailUnfinished marks a still-running ticket failed and fills in its run's
// missing end time. Other ticket statuses are preserved; a missing run
// returns ErrNoRun.
//
// Supervisors call this with their claimed run ID on exit. Only dg finish
// marks work ready, so exiting without a report is a failure. Reconcile
// handles supervisors that crash before this call.
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

// endRunAt fills in a missing run end time for cancellation or failure
// recovery. Existing end times are preserved. A zero run ID means there is no
// run to close.
//
// Unlike EndRun, this accepts the caller's timestamp, does not record an exit
// code, and tolerates a missing run.
func endRunAt(tx *sql.Tx, runID int64, at time.Time) error {
	_, err := tx.Exec(
		"UPDATE runs SET ended_at = ? WHERE id = ? AND ended_at IS NULL",
		rfc3339(at), runID)
	return err
}

// failRun marks a ticket failed and closes the specified run. Use the
// caller's run ID because a concurrent restart may have created a newer run.
func failRun(tx *sql.Tx, runID, ticketID int64, at time.Time) error {
	if err := changeStatus(tx, ticketID, Failed, at); err != nil {
		return err
	}
	return endRunAt(tx, runID, at)
}

// Cancel marks a non-terminal ticket cancelled and fills in the specified
// run's end time. Done and cancelled tickets return
// ErrInvalidTicketStateChange.
//
// Stop the supervisor before calling Cancel: waiting for signals while
// holding SQLite's writer lock would block other commands. Pass the ID of the
// run that was stopped, not the latest run, which could belong to a
// concurrent restart. Pass zero when there is no run to close.
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

// FinishTicket records a commit and moves Running or Failed tickets to Ready.
// This allows a failed session resumed with dg chat to finish.
//
// For an already Ready ticket, only the commit changes, preserving its
// position and status timestamp after an amend or rebase. Other statuses
// return ErrInvalidTicketStateChange.
func (s *Store) FinishTicket(id int64, commit string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	status, err := statusOf(tx, id)
	if err != nil {
		return err
	}
	if status != Ready {
		if err := changeStatus(tx, id, Ready, time.Now()); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(
		"UPDATE tickets SET commit_id = ? WHERE id = ?", commit, id,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// ChangeStatusWith applies a status change and calls work before committing.
// If work fails, the database change rolls back. For example, dg accept must
// leave a ticket ready when removing its worktree fails.
//
// The callback holds SQLite's writer lock, so keep it brief. External side
// effects cannot be rolled back by the database.
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

// SetSession records the agent session ID on a ticket so the conversation can
// be reopened later.
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

// SetTitle updates the ticket title, which is stored separately from its
// description.
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

// IsQueueRunning reports whether the queue may start new tickets.
func (s *Store) IsQueueRunning() (bool, error) {
	var running bool
	err := s.db.QueryRow("SELECT running FROM queue_state WHERE id = 1").Scan(&running)
	if err != nil {
		return false, err
	}
	return running, nil
}

// setQueueRunning enables or disables automatic starts. Pausing leaves
// existing runs active.
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
