package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// emptyStore returns a store that holds one project and no ticket, with the id of
// the project.
func emptyStore(t *testing.T) (*Store, int64) {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
		t.Cleanup(func() { s.Close() })
	}

	projectID, err := s.AddProject("/projects/path", "main")
	if err != nil {
		t.Fatal(err)
	}
	return s, projectID
}

// oneTicket returns a store that holds one project and one ticket named
// "My Ticket", with the id of the ticket.
func oneTicket(t *testing.T) (*Store, int64) {
	t.Helper()
	s, projectID := emptyStore(t)
	id, err := s.AddTicket(projectID, "My Ticket")
	if err != nil {
		t.Fatal(err)
	}
	return s, id
}

// threeTickets returns a store that holds one project and three tickets, in the
// order first, second, third, with their ids.
func threeTickets(t *testing.T) (*Store, []int64) {
	t.Helper()
	s, projectID := emptyStore(t)
	ids := make([]int64, 0, 3)
	for _, title := range []string{"first", "second", "third"} {
		id, err := s.AddTicket(projectID, title)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return s, ids
}

// twoPrograms returns two stores on one data directory, each with its own pool of
// connections, and the ids of six tickets in the queue. Two stores are what two
// programs of delegator have.
func twoPrograms(t *testing.T) (*Store, *Store, []int64) {
	t.Helper()
	dir := t.TempDir()
	first, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { first.Close() })
	second, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { second.Close() })

	projectID, err := first.AddProject("/projects/path", "main")
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]int64, 0, 6)
	for i := range 6 {
		id, err := first.AddTicket(projectID, fmt.Sprintf("ticket %d", i))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return first, second, ids
}

// mustProject returns the id of the one project that a fixture made.
func mustProject(t *testing.T, s *Store) int64 {
	t.Helper()
	projects, err := s.Projects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 {
		t.Fatalf("the database holds %d projects, want one", len(projects))
	}
	return projects[0].ID
}

// queueTitles returns the title of each ticket of the queue, in its order.
func queueTitles(t *testing.T, s *Store) []string {
	t.Helper()
	queue, err := s.ListQueue()
	if err != nil {
		t.Fatal(err)
	}
	titles := make([]string, 0, len(queue))
	for _, ticket := range queue {
		titles = append(titles, ticket.Title)
	}
	return titles
}

// ticketPosition returns the position of one ticket. A ticket that is not in
// the queue holds no position, and the value is then not valid.
func ticketPosition(t *testing.T, s *Store, id int64) sql.Null[int] {
	t.Helper()
	var position sql.Null[int]
	if err := s.db.QueryRow(
		"SELECT position FROM tickets WHERE id = ?", id).Scan(&position); err != nil {
		t.Fatal(err)
	}
	return position
}

// openRaw opens the database with no migration, so a test can examine what
// Open left behind.
func openRaw(t *testing.T, dataDir string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "delegator.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func userVersion(t *testing.T, db *sql.DB) int {
	t.Helper()
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	return version
}

// setMigrations puts a different list of steps in place for one test. Each test
// of this package runs one after the other, so no test sees the list of another.
func setMigrations(t *testing.T, list []string) {
	t.Helper()
	old := migrations
	migrations = list
	t.Cleanup(func() { migrations = old })
}

func TestOpenMakesTheDatabaseAndTheTables(t *testing.T) {
	dataDir := t.TempDir()

	s, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// The name of the file is a literal, because a change of the name loses the
	// data of each person who has delegator now.
	if _, err := os.Stat(filepath.Join(dataDir, "delegator.db")); err != nil {
		t.Error(err)
	}

	for _, name := range []string{"projects", "tickets"} {
		var got string
		err := s.db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", name).Scan(&got)
		if err != nil {
			t.Errorf("table %s: %v", name, err)
		}
	}
}

func TestOpenMakesTheDirectories(t *testing.T) {
	// A directory below the temporary directory, because Open must make the
	// data directory itself and not only the directories below it.
	dataDir := filepath.Join(t.TempDir(), "delegator")

	s, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// The empty name is the data directory. The test says the permission 0700 a
	// second time as a literal. A test that read the constant would stay green
	// after a change of it, because that one change moves the value that the
	// test wants at the same time.
	for _, name := range []string{"", "tickets", "worktrees", "runs"} {
		info, err := os.Stat(filepath.Join(dataDir, name))
		if err != nil {
			t.Errorf("%q: %v", name, err)
			continue
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Errorf("%q permission = %v, want %v", name, got, os.FileMode(0o700))
		}
	}
}

func TestOpenWithTheDataDirectoryAlreadyPresent(t *testing.T) {
	dataDir := t.TempDir()
	s, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := os.Stat(filepath.Join(dataDir, "tickets")); err != nil {
		t.Fatal(err)
	}
}

func TestOpenWritesTheVersionOfTheLastStep(t *testing.T) {
	dataDir := t.TempDir()
	s, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if got := userVersion(t, s.db); got != len(migrations) {
		t.Errorf("user_version = %d, want %d", got, len(migrations))
	}
}

// One transaction holds each step, so a step that gives an error part way
// leaves the database as it was.
func TestOpenWithAStepThatFailsChangesNothing(t *testing.T) {
	dataDir := t.TempDir()
	first, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	first.Close()

	// The new step makes a table, and then gives an error. Neither statement
	// must stay.
	good := len(migrations)
	setMigrations(t, append(migrations,
		"CREATE TABLE later (id INTEGER PRIMARY KEY);\nSELECT no_such_function();"))

	if _, err := Open(dataDir); err == nil {
		t.Fatal("Open gave no error")
	}

	db := openRaw(t, dataDir)
	if got := userVersion(t, db); got != good {
		t.Errorf("user_version = %d, want %d", got, good)
	}
	var name string
	err = db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'later'").Scan(&name)
	if err == nil {
		t.Error("the table later stayed after the step gave an error")
	}
}

// A later version of delegator can add a step, and a person can then start an
// earlier version. The earlier version knows nothing about the new step, so it
// must stop and write nothing.
func TestOpenWithADatabaseFromALaterVersion(t *testing.T) {
	dataDir := t.TempDir()
	first, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	first.Close()

	// What a later version of delegator leaves behind.
	db := openRaw(t, dataDir)
	if _, err := db.Exec("CREATE TABLE later (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	later := len(migrations) + 1
	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", later)); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(dataDir); !errors.Is(err, ErrNewerDatabase) {
		t.Errorf("err = %v, want %v", err, ErrNewerDatabase)
	}

	if got := userVersion(t, db); got != later {
		t.Errorf("user_version = %d, want %d", got, later)
	}
	var name string
	if err := db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'later'").Scan(&name); err != nil {
		t.Errorf("the table of the later version went away: %v", err)
	}
}

func TestOpenSetsWalMode(t *testing.T) {
	dataDir := t.TempDir()
	s, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	var mode string
	if err := s.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want %q", mode, "wal")
	}
}

// busy_timeout belongs to one connection, and database/sql keeps a pool that
// can make a new connection at any time. Each connection must therefore get the
// value. The test holds the first connection while it takes the second, because
// the pool gives the same connection again if the first one is free.
func TestOpenSetsBusyTimeoutOnEachConnection(t *testing.T) {
	dataDir := t.TempDir()
	s, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()
	for i := range 2 {
		conn, err := s.db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()

		var timeout int
		if err := conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&timeout); err != nil {
			t.Fatal(err)
		}
		if timeout != 5000 {
			t.Errorf("connection %d: busy_timeout = %d, want 5000", i, timeout)
		}
	}
}

// The path of the database goes in a URL, and a path can hold the characters
// that separate a URL. SQLite reads such a path only as far as that character,
// and it makes the database at a different place with no error.
func TestOpenWithAPathThatHoldsURLCharacters(t *testing.T) {
	for _, name := range []string{"my data", "we#ird", "qu?ery"} {
		t.Run(name, func(t *testing.T) {
			dataDir := filepath.Join(t.TempDir(), name)

			s, err := Open(dataDir)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()

			if _, err := os.Stat(filepath.Join(dataDir, "delegator.db")); err != nil {
				t.Errorf("the database is not at the path that Open got: %v", err)
			}
		})
	}
}

func TestAddProjectSucceeds(t *testing.T) {
	dataDir := t.TempDir()
	s, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	projectID, err := s.AddProject("/projects/path", "main")
	if err != nil {
		t.Fatal(err)
	}

	type Project struct {
		ID            int64
		Path          string
		DefaultBranch string
	}

	var project Project
	row := s.db.QueryRow("SELECT id, path, default_branch FROM projects WHERE id = ?", projectID)
	if err := row.Scan(
		&project.ID,
		&project.Path,
		&project.DefaultBranch,
	); err != nil {
		t.Fatal(err)
	}

	if project.Path != "/projects/path" {
		t.Errorf("path = %s, want = /projects/path", project.Path)
	}
	if project.DefaultBranch != "main" {
		t.Errorf("default_branch = %s, want = main", project.DefaultBranch)
	}
}

func TestAddTicketSucceeds(t *testing.T) {
	s, projectID := emptyStore(t)

	ticketID, err := s.AddTicket(projectID, "My Ticket")
	if err != nil {
		t.Fatal(err)
	}

	if ticketID != 1 {
		t.Errorf("ticketID = %d, want = %d", ticketID, 1)
	}

	type Ticket struct {
		ID        int64
		ProjectID int64
		Title     string
		State     string
		Created   string
	}

	var ticket Ticket
	row := s.db.QueryRow("SELECT id, project_id, title, status, created FROM tickets WHERE id = ?", ticketID)
	if err = row.Scan(
		&ticket.ID,
		&ticket.ProjectID,
		&ticket.Title,
		&ticket.State,
		&ticket.Created,
	); err != nil {
		t.Fatal(err)
	}

	if ticket.ProjectID != projectID {
		t.Errorf("project_id = %d, want = %d", ticket.ProjectID, projectID)
	}
	if ticket.Title != "My Ticket" {
		t.Errorf("title = %s, want = My Ticket", ticket.Title)
	}
	if ticket.State != "queued" {
		t.Errorf("state = %s, want = queued", ticket.State)
	}

	rfc3339Pattern := `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`
	matched, err := regexp.MatchString(rfc3339Pattern, ticket.Created)
	if err != nil {
		t.Fatal(err)
	}
	if !matched {
		t.Errorf("created does not match RFC3339 format: %s", ticket.Created)
	}
}

func TestAddTicketWithInvalidProjectFails(t *testing.T) {
	dataDir := t.TempDir()
	s, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	_, err = s.AddTicket(9999, "My Ticket")
	var sErr *sqlite.Error
	if !errors.As(err, &sErr) {
		t.Fatalf("err is %T, want *sqlite.Error", err)
	}
	if sErr.Code() != sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY {
		t.Errorf("code = %d, want %d", sErr.Code(), sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY)
	}
}

func TestQueueGivesATicketThatHasAPosition(t *testing.T) {
	s, ticketID := oneTicket(t)
	if _, err := s.db.Exec("UPDATE tickets SET position = 1 WHERE id = ?", ticketID); err != nil {
		t.Fatal(err)
	}

	queue, err := s.ListQueue()
	if err != nil {
		t.Fatal(err)
	}

	if len(queue) != 1 {
		t.Fatalf("the queue holds %d tickets, want 1", len(queue))
	}
	if queue[0].ID != ticketID {
		t.Errorf("ID = %d, want %d", queue[0].ID, ticketID)
	}
	if queue[0].Title != "My Ticket" {
		t.Errorf("title = %s, want My Ticket", queue[0].Title)
	}
}

func TestQueueLeavesOutATicketThatHasNoPosition(t *testing.T) {
	s, ticketID := oneTicket(t)
	if _, err := s.RemoveTicket(ticketID, Running); err != nil {
		t.Fatal(err)
	}

	if got := queueTitles(t, s); len(got) != 0 {
		t.Errorf("the queue is %v, want no ticket", got)
	}
}

func TestQueueGivesTheOrderOfPosition(t *testing.T) {
	s, ids := threeTickets(t)

	// The position of each ticket runs against its id. A queue in the order
	// of id therefore looks different from a queue in the order of position,
	// and the test can tell the two apart. Each position goes below zero first,
	// because the column has a unique index and the CHECK of the table refuses a
	// queued ticket with no position.
	if _, err := s.db.Exec("UPDATE tickets SET position = -position"); err != nil {
		t.Fatal(err)
	}
	for i, id := range ids {
		if _, err := s.db.Exec(
			"UPDATE tickets SET position = ? WHERE id = ?", len(ids)-i, id); err != nil {
			t.Fatal(err)
		}
	}

	want := []string{"third", "second", "first"}
	if got := queueTitles(t, s); !slices.Equal(got, want) {
		t.Errorf("the queue is %v, want %v", got, want)
	}
}

func TestOpenStopsTwoTicketsFromSharingAPosition(t *testing.T) {
	s, projectID := emptyStore(t)
	first, err := s.AddTicket(projectID, "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.AddTicket(projectID, "second")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.db.Exec("UPDATE tickets SET position = 1 WHERE id = ?", first); err != nil {
		t.Fatal(err)
	}

	_, err = s.db.Exec("UPDATE tickets SET position = 1 WHERE id = ?", second)
	var sErr *sqlite.Error
	if !errors.As(err, &sErr) {
		t.Fatalf("err is %T, want *sqlite.Error", err)
	}
	if sErr.Code() != sqlite3.SQLITE_CONSTRAINT_UNIQUE {
		t.Errorf("code = %d, want %d", sErr.Code(), sqlite3.SQLITE_CONSTRAINT_UNIQUE)
	}
}

// The database of a person who has an earlier version of delegator holds only
// the steps of that version. The next start must apply each step that is
// missing, and no step that is present. This is the path that such a database
// takes.
func TestOpenAppliesANewStepToAnOldDatabase(t *testing.T) {
	dataDir := t.TempDir()

	old := migrations
	migrations = old[:1]
	first, err := Open(dataDir)
	migrations = old
	if err != nil {
		t.Fatal(err)
	}
	first.Close()

	if got := userVersion(t, openRaw(t, dataDir)); got != 1 {
		t.Fatalf("the old database is at version %d, want 1", got)
	}

	second, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	if got := userVersion(t, second.db); got != len(migrations) {
		t.Errorf("user_version = %d, want %d", got, len(migrations))
	}
	var name string
	err = second.db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type = 'index' AND name = 'tickets_position'").Scan(&name)
	if err != nil {
		t.Errorf("the index of the second step is not on the old database: %v", err)
	}
}

func TestAddTicketPutsTheTicketAtTheEndOfTheQueue(t *testing.T) {
	s, ids := threeTickets(t)

	want := []string{"first", "second", "third"}
	if got := queueTitles(t, s); !slices.Equal(got, want) {
		t.Errorf("the queue is %v, want %v", got, want)
	}

	// The order starts at 1. The queue works with any first number, but a
	// person who reads the table sees these numbers.
	if position := ticketPosition(t, s, ids[0]); position.V != 1 {
		t.Errorf("the first ticket is at position %d, want 1", position.V)
	}
}

func TestRemoveTicketRemovesTicketFromQueue(t *testing.T) {
	s, ids := threeTickets(t)

	removeID := ids[0]
	ok, err := s.RemoveTicket(removeID, Running)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("ticket was not removed")
	}

	want := []string{"second", "third"}
	if got := queueTitles(t, s); !slices.Equal(got, want) {
		t.Errorf("the queue is %v, want %v", got, want)
	}

	// check that the removed ticket has the values set correctly
	var state string
	if err = s.db.QueryRow(
		"SELECT status FROM tickets WHERE id = ?", removeID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "running" {
		t.Errorf("state = %s, want = running", state)
	}
	if position := ticketPosition(t, s, removeID); position.Valid {
		t.Errorf("position = %d, want = nil", position.V)
	}
}

func TestRemoveTicketCanOnlyTransitionToValidStates(t *testing.T) {
	s, id := oneTicket(t)

	for _, state := range []TicketStatus{Ready, Failed, Done, Queued} {
		if _, err := s.RemoveTicket(id, state); !errors.Is(err, ErrInvalidTicketStateChange) {
			t.Errorf("RemoveTicket to %s gave err = %v, want ErrInvalidTicketStateChange", state, err)
		}

		tickets, err := s.ListQueue()
		if err != nil {
			t.Fatal(err)
		}
		if len(tickets) != 1 {
			t.Errorf("ticket should remain in queue, got length = %d for state = %s", len(tickets), state)
		}
	}
}

func TestRemoveTicketDoesNotModifyANonQueuedTicket(t *testing.T) {
	s, projectID := emptyStore(t)

	nonEntryState := Ready // a state that can't advance from queued
	result, err := s.db.Exec(
		"INSERT INTO tickets (project_id, title, status, created) VALUES (?, ?, ?, ?)",
		projectID, "title", nonEntryState, "2026-08-27",
	)
	if err != nil {
		t.Fatal(err)
	}

	ticketID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	ok, err := s.RemoveTicket(ticketID, Running)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("ticket was removed and should not have been")
	}
}

func TestMoveTicketWritesTheNewOrder(t *testing.T) {
	s, ids := threeTickets(t)

	if err := s.MoveTicket(ids[2], Top); err != nil {
		t.Fatal(err)
	}

	want := []string{"third", "first", "second"}
	if got := queueTitles(t, s); !slices.Equal(got, want) {
		t.Errorf("the queue is %v, want %v", got, want)
	}

	if position := ticketPosition(t, s, ids[2]); position.V != 1 {
		t.Errorf("the first ticket of the queue is at position %d, want 1", position.V)
	}
}

func TestMoveTicketMovesInEachDirection(t *testing.T) {
	tests := []struct {
		move Move
		want []string
	}{
		{Up, []string{"second", "first", "third"}},
		{Down, []string{"first", "third", "second"}},
		{Top, []string{"second", "first", "third"}},
		{Bottom, []string{"first", "third", "second"}},
	}
	for _, test := range tests {
		t.Run(string(test.move), func(t *testing.T) {
			s, ids := threeTickets(t)
			if err := s.MoveTicket(ids[1], test.move); err != nil {
				t.Fatal(err)
			}
			if got := queueTitles(t, s); !slices.Equal(got, test.want) {
				t.Errorf("the queue is %v, want %v", got, test.want)
			}
		})
	}
}

func TestMoveTicketThatIsNotInTheQueueChangesNothing(t *testing.T) {
	s, ids := threeTickets(t)
	if _, err := s.RemoveTicket(ids[1], Running); err != nil {
		t.Fatal(err)
	}
	before := queueTitles(t, s)

	err := s.MoveTicket(ids[1], Top)
	if !errors.Is(err, ErrNotInTheQueue) {
		t.Errorf("err = %v, want ErrNotInTheQueue", err)
	}
	if got := queueTitles(t, s); !slices.Equal(got, before) {
		t.Errorf("the queue is %v, want %v", got, before)
	}
}

// A ticket of the queue holds the status queued and a position. Each one alone
// puts the ticket in no queue: a ticket with a position and another status left
// the queue, and a ticket with the status queued and no position is in no queue
// at all. The database refuses each half, so no command can make one.
func TestTheDatabaseRefusesATicketThatIsHalfInTheQueue(t *testing.T) {
	s, ids := threeTickets(t)

	tests := []struct {
		name  string
		query string
	}{
		{"a status that is not queued, with the position left behind",
			"UPDATE tickets SET status = 'running' WHERE id = ?"},
		{"the status queued, with the position taken away",
			"UPDATE tickets SET position = NULL WHERE id = ?"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := s.db.Exec(test.query, ids[1])
			var sErr *sqlite.Error
			if !errors.As(err, &sErr) {
				t.Fatalf("err is %T, want *sqlite.Error", err)
			}
			if sErr.Code() != sqlite3.SQLITE_CONSTRAINT_CHECK {
				t.Errorf("code = %d, want %d", sErr.Code(), sqlite3.SQLITE_CONSTRAINT_CHECK)
			}
		})
	}

	if got := queueTitles(t, s); !slices.Equal(got, []string{"first", "second", "third"}) {
		t.Errorf("the queue is %v, and it must not change", got)
	}
}

func TestTwoProgramsThatWriteAtTheSameTimeLoseNoTicket(t *testing.T) {
	first, second, ids := twoPrograms(t)

	const moves = 20
	var wg sync.WaitGroup
	// Each move sends its error here. Two goroutines cannot add to a slice at
	// the same time, and one of the two writes goes away. The channel holds one
	// place for each move, so a send never waits for a read, and nothing reads
	// until each move has stopped.
	errs := make(chan error, moves)
	for i := range moves {
		program := first
		if i%2 == 1 {
			program = second
		}
		// Add is here and not below, because Wait can see a count of zero
		// before the first goroutine has run, and return at once.
		wg.Add(1)
		go func(s *Store, n int) {
			defer wg.Done()
			if err := s.MoveTicket(ids[n%len(ids)], Top); err != nil {
				errs <- err
			}
		}(program, i)
	}
	wg.Wait()
	close(errs)

	var failed []error
	for err := range errs {
		failed = append(failed, err)
	}
	if len(failed) > 0 {
		t.Errorf("%d of %d moves gave an error; the first was %v", len(failed), moves, failed[0])
	}

	// Each ticket is still in the queue, and the positions are 1 to 6 with no
	// gap. A gap is a write that another write lost.
	if got := len(queueTitles(t, first)); got != len(ids) {
		t.Fatalf("the queue holds %d tickets, want %d", got, len(ids))
	}
	var low, high, count int
	if err := first.db.QueryRow(
		`SELECT MIN(position), MAX(position), count(*) FROM tickets
		 WHERE position IS NOT NULL`).Scan(&low, &high, &count); err != nil {
		t.Fatal(err)
	}
	if low != 1 || high != count {
		t.Errorf("the positions run from %d to %d over %d tickets, want 1 to %d", low, high, count, count)
	}
}

func TestProjectIDMakesTheProjectOnce(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	first, err := s.ProjectID("/projects/path", "main")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.ProjectID("/projects/path", "trunk")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("the second call gives %d, want %d", second, first)
	}

	projects, err := s.Projects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 {
		t.Fatalf("the database holds %d projects, want 1", len(projects))
	}
	if projects[0].Path != "/projects/path" {
		t.Errorf("path = %q, want /projects/path", projects[0].Path)
	}
	// The first call settles the branch. The second gave a different one, and
	// the project keeps the branch that it has.
	if projects[0].DefaultBranch != "main" {
		t.Errorf("default branch = %q, want main", projects[0].DefaultBranch)
	}
}

func TestProjectIDGivesADifferentIDForADifferentPath(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	first, err := s.ProjectID("/projects/one", "main")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.ProjectID("/projects/two", "main")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Errorf("both projects have id %d, want two ids", first)
	}
}

// A ticket can hold private data, so only the person who made it can read the
// database. SQLite makes the file, and it takes the umask of the person, so
// Open must set the permission itself.
func TestOpenMakesTheDatabaseForItsPersonOnly(t *testing.T) {
	dataDir := t.TempDir()
	s, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	info, err := os.Stat(filepath.Join(dataDir, "delegator.db"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("the permission is %v, want %v", got, os.FileMode(0o600))
	}
}

// setStatus takes a ticket out of the queue and sets its status. No command
// makes a ticket ready yet, because that is dg finish in milestone 2.
func setStatus(t *testing.T, s *Store, id int64, status TicketStatus) {
	t.Helper()
	if _, err := s.db.Exec(
		"UPDATE tickets SET status = ?, position = NULL WHERE id = ?", status, id); err != nil {
		t.Fatal(err)
	}
}

func TestOpenTicketsGivesEachTicketThatIsNotClosed(t *testing.T) {
	s, ids := threeTickets(t)
	setStatus(t, s, ids[0], Ready)
	setStatus(t, s, ids[1], Running)
	// the third stays queued

	// a ticket that is closed is not in the inbox
	closed, err := s.AddTicket(mustProject(t, s), "closed")
	if err != nil {
		t.Fatal(err)
	}
	setStatus(t, s, closed, Done)

	open, err := s.OpenTickets()
	if err != nil {
		t.Fatal(err)
	}

	byID := map[int64]OpenTicket{}
	for _, ticket := range open {
		byID[ticket.ID] = ticket
	}
	if len(byID) != 3 {
		t.Fatalf("OpenTickets gives %d tickets, want 3", len(byID))
	}
	for _, want := range []struct {
		id     int64
		status TicketStatus
	}{{ids[0], Ready}, {ids[1], Running}, {ids[2], Queued}} {
		if got := byID[want.id].Status; got != want.status {
			t.Errorf("ticket %d has the status %q, want %q", want.id, got, want.status)
		}
	}
	if _, there := byID[closed]; there {
		t.Error("OpenTickets gives a ticket that is done")
	}
}

func TestOpenTicketsGivesTheProjectAndTheTitle(t *testing.T) {
	s, ids := threeTickets(t)

	open, err := s.OpenTickets()
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 3 {
		t.Fatalf("OpenTickets gives %d tickets, want 3", len(open))
	}
	for _, ticket := range open {
		if ticket.Project != "/projects/path" {
			t.Errorf("ticket %d has the project %q, want /projects/path", ticket.ID, ticket.Project)
		}
	}
	byID := map[int64]OpenTicket{}
	for _, ticket := range open {
		byID[ticket.ID] = ticket
	}
	if got := byID[ids[0]].Title; got != "first" {
		t.Errorf("title = %q, want first", got)
	}
}

// The inbox shows each project together, so one query returns the tickets of two
// projects.
func TestOpenTicketsGivesTheTicketsOfEachProject(t *testing.T) {
	s, projectID := emptyStore(t)
	other, err := s.ProjectID("/projects/other", "main")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddTicket(projectID, "one"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddTicket(other, "two"); err != nil {
		t.Fatal(err)
	}

	open, err := s.OpenTickets()
	if err != nil {
		t.Fatal(err)
	}
	// A join that is not on the project returns each ticket with each project,
	// so the count matters as much as the paths.
	if len(open) != 2 {
		t.Fatalf("OpenTickets gives %d tickets, want 2", len(open))
	}
	byTitle := map[string]string{}
	for _, ticket := range open {
		byTitle[ticket.Title] = ticket.Project
	}
	if got := byTitle["one"]; got != "/projects/path" {
		t.Errorf("the ticket one has the project %q, want /projects/path", got)
	}
	if got := byTitle["two"]; got != "/projects/other" {
		t.Errorf("the ticket two has the project %q, want /projects/other", got)
	}
}

// setCompleted writes the time that a ticket became ready. dg finish does this
// work in milestone 2.
func setCompleted(t *testing.T, s *Store, id int64, completed string) {
	t.Helper()
	if _, err := s.db.Exec(
		"UPDATE tickets SET completed = ? WHERE id = ?", completed, id); err != nil {
		t.Fatal(err)
	}
}

func TestOpenTicketsGivesTheTimeOfCompletion(t *testing.T) {
	s, ids := threeTickets(t)
	setStatus(t, s, ids[0], Ready)
	setCompleted(t, s, ids[0], "2026-08-28T09:30:00Z")

	open, err := s.OpenTickets()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[int64]OpenTicket{}
	for _, ticket := range open {
		byID[ticket.ID] = ticket
	}
	if got := byID[ids[0]].Completed; got != "2026-08-28T09:30:00Z" {
		t.Errorf("completed = %q, want 2026-08-28T09:30:00Z", got)
	}
	// a ticket that waits has no time of completion
	if got := byID[ids[2]].Completed; got != "" {
		t.Errorf("a queued ticket has completed = %q, want it empty", got)
	}
}

func TestTicketReturnsEachFieldOfOneRow(t *testing.T) {
	s, ids := threeTickets(t)
	setStatus(t, s, ids[1], Ready)
	setCompleted(t, s, ids[1], "2026-08-28T09:30:00Z")
	if _, err := s.db.Exec(
		`UPDATE tickets SET branch = ?, session = ?, result = ?, flags = ? WHERE id = ?`,
		"delegator/2-second", "a-session-id", "it is done", "none", ids[1]); err != nil {
		t.Fatal(err)
	}

	got, err := s.Ticket(ids[1])
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, got, want string }{
		{"project", got.Project, "/projects/path"},
		{"title", got.Title, "second"},
		{"status", string(got.Status), "ready"},
		{"branch", got.Branch, "delegator/2-second"},
		{"session", got.Session, "a-session-id"},
		{"result", got.Result, "it is done"},
		{"flags", got.Flags, "none"},
		{"completed", got.Completed, "2026-08-28T09:30:00Z"},
	} {
		if test.got != test.want {
			t.Errorf("%s = %q, want %q", test.name, test.got, test.want)
		}
	}
	if got.ID != ids[1] {
		t.Errorf("id = %d, want %d", got.ID, ids[1])
	}
	if got.Created == "" {
		t.Error("created is empty")
	}
}

// A ticket that is not in the queue holds no position, and a column that holds
// NULL arrives as the zero value of its type.
func TestTicketWithNoPositionAndNoBranch(t *testing.T) {
	s, id := oneTicket(t)
	if _, err := s.RemoveTicket(id, Running); err != nil {
		t.Fatal(err)
	}

	got, err := s.Ticket(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Position != 0 {
		t.Errorf("position = %d, want 0", got.Position)
	}
	if got.Branch != "" || got.Session != "" || got.Result != "" || got.Flags != "" {
		t.Errorf("a ticket that had no run holds %+v", got)
	}
}

func TestTicketThatIsNotThere(t *testing.T) {
	s, _ := emptyStore(t)

	_, err := s.Ticket(9999)
	if !errors.Is(err, ErrNoTicket) {
		t.Errorf("err = %v, want ErrNoTicket", err)
	}
}

func TestMoveTicketBeforePutsItWhereTheTargetIs(t *testing.T) {
	s, ids := threeTickets(t)

	// first goes where third is, so third goes down one place
	if err := s.MoveTicketBefore(ids[0], ids[2]); err != nil {
		t.Fatal(err)
	}
	want := []string{"second", "first", "third"}
	if got := queueTitles(t, s); !slices.Equal(got, want) {
		t.Errorf("the queue is %v, want %v", got, want)
	}
}

func TestMoveTicketBeforeUpTheQueue(t *testing.T) {
	s, ids := threeTickets(t)

	if err := s.MoveTicketBefore(ids[2], ids[0]); err != nil {
		t.Fatal(err)
	}
	want := []string{"third", "first", "second"}
	if got := queueTitles(t, s); !slices.Equal(got, want) {
		t.Errorf("the queue is %v, want %v", got, want)
	}
}

func TestMoveTicketBeforeItself(t *testing.T) {
	s, ids := threeTickets(t)
	before := queueTitles(t, s)

	if err := s.MoveTicketBefore(ids[1], ids[1]); err != nil {
		t.Fatal(err)
	}
	if got := queueTitles(t, s); !slices.Equal(got, before) {
		t.Errorf("the queue is %v, want %v", got, before)
	}
}

func TestMoveTicketBeforeWithATargetThatIsNotInTheQueue(t *testing.T) {
	s, ids := threeTickets(t)
	if _, err := s.RemoveTicket(ids[2], Running); err != nil {
		t.Fatal(err)
	}
	before := queueTitles(t, s)

	err := s.MoveTicketBefore(ids[0], ids[2])
	if !errors.Is(err, ErrNotInTheQueue) {
		t.Errorf("err = %v, want ErrNotInTheQueue", err)
	}
	if got := queueTitles(t, s); !slices.Equal(got, before) {
		t.Errorf("the queue is %v, want %v", got, before)
	}
}

func TestMoveTicketBeforeWithATicketThatIsNotInTheQueue(t *testing.T) {
	s, ids := threeTickets(t)
	if _, err := s.RemoveTicket(ids[0], Running); err != nil {
		t.Fatal(err)
	}

	if err := s.MoveTicketBefore(ids[0], ids[2]); !errors.Is(err, ErrNotInTheQueue) {
		t.Errorf("err = %v, want ErrNotInTheQueue", err)
	}
}

// A ticket that runs can become ready, which is what dg finish does. Both hold
// no position, so this change does not need the work of condition 2.
func TestChangeStatusWritesTheNewStatus(t *testing.T) {
	s, id := oneTicket(t)
	if _, err := s.RemoveTicket(id, Running); err != nil {
		t.Fatal(err)
	}

	if err := s.ChangeStatus(id, Ready); err != nil {
		t.Fatal(err)
	}

	ticket, err := s.Ticket(id)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Status != Ready {
		t.Errorf("status = %q, want %q", ticket.Status, Ready)
	}
}

// nextStates says which change one status can take, and ChangeStatus obeys it.
func TestChangeStatusWithAChangeThatNextStatesDoesNotHold(t *testing.T) {
	s, id := oneTicket(t)
	if _, err := s.RemoveTicket(id, Running); err != nil {
		t.Fatal(err)
	}

	// running goes to ready, failed or cancelled, and not to done
	err := s.ChangeStatus(id, Done)
	if !errors.Is(err, ErrInvalidTicketStateChange) {
		t.Fatalf("err = %v, want ErrInvalidTicketStateChange", err)
	}
	// the message names the two, so the person sees which change it refused
	for _, want := range []string{string(Running), string(Done)} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, and it does not name %q", err, want)
		}
	}

	ticket, err := s.Ticket(id)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Status != Running {
		t.Errorf("status = %q, and the change that gave an error wrote it", ticket.Status)
	}
}

// An end is an end: no change leaves done or cancelled.
func TestChangeStatusFromAnEnd(t *testing.T) {
	s, id := oneTicket(t)
	if _, err := s.RemoveTicket(id, Cancelled); err != nil {
		t.Fatal(err)
	}

	if err := s.ChangeStatus(id, Running); !errors.Is(err, ErrInvalidTicketStateChange) {
		t.Errorf("err = %v, want ErrInvalidTicketStateChange", err)
	}
}

// The CHECK of the table holds a position for a queued ticket and none for
// every other status, so a ticket that leaves the queue gives up its position.
func TestChangeStatusOutOfTheQueueClearsThePosition(t *testing.T) {
	s, id := oneTicket(t)

	if err := s.ChangeStatus(id, Running); err != nil {
		t.Fatal(err)
	}

	if position := ticketPosition(t, s, id); position.Valid {
		t.Errorf("position = %d, want none", position.V)
	}
}

// dg revise puts a ticket that a person rejected back into the queue. It goes
// to the end, behind each ticket that already waits.
func TestChangeStatusIntoTheQueuePutsTheTicketAtTheEnd(t *testing.T) {
	s, ids := threeTickets(t)

	for _, status := range []TicketStatus{Running, Ready, Queued} {
		if err := s.ChangeStatus(ids[0], status); err != nil {
			t.Fatal(err)
		}
	}

	want := []string{"second", "third", "first"}
	if got := queueTitles(t, s); !slices.Equal(got, want) {
		t.Errorf("the queue is %v, want %v", got, want)
	}
}
