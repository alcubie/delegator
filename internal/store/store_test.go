package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

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

func TestOpenWritesTheVersionOfTheLastStep(t *testing.T) {
	dataDir := t.TempDir()
	s, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// The literal 2 is the count of steps that this version has. A test that
	// read len(migrations) would stay green after a new step, because that one
	// change moves the value that the test wants at the same time.
	if got := userVersion(t, s.db); got != 2 {
		t.Errorf("user_version = %d, want 2", got)
	}
}

func TestOpenAgainAppliesNoStepAgain(t *testing.T) {
	dataDir := t.TempDir()
	first, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	first.Close()

	second, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	if got := userVersion(t, second.db); got != 2 {
		t.Errorf("user_version = %d, want 2", got)
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

	// The second step makes a table, and then gives an error. Neither statement
	// must stay.
	setMigrations(t, append(migrations,
		"CREATE TABLE later (id INTEGER PRIMARY KEY);\nSELECT no_such_function();"))

	if _, err := Open(dataDir); err == nil {
		t.Fatal("Open gave no error")
	}

	db := openRaw(t, dataDir)
	if got := userVersion(t, db); got != 2 {
		t.Errorf("user_version = %d, want 2", got)
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
	if _, err := db.Exec("PRAGMA user_version = 3"); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(dataDir); !errors.Is(err, ErrNewerDatabase) {
		t.Errorf("err = %v, want %v", err, ErrNewerDatabase)
	}

	if got := userVersion(t, db); got != 3 {
		t.Errorf("user_version = %d, want 3", got)
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
	row := s.db.QueryRow("SELECT id, project_id, title, state, created FROM tickets WHERE id = ?", ticketID)
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
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	projectID, err := s.AddProject("/projects/path", "main")
	if err != nil {
		t.Fatal(err)
	}
	ticketID, err := s.AddTicket(projectID, "My Ticket")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("UPDATE tickets SET position = 1 WHERE id = ?", ticketID); err != nil {
		t.Fatal(err)
	}

	queue, err := s.Queue()
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
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	projectID, err := s.AddProject("/projects/path", "main")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddTicket(projectID, "My Ticket"); err != nil {
		t.Fatal(err)
	}

	queue, err := s.Queue()
	if err != nil {
		t.Fatal(err)
	}

	if len(queue) != 0 {
		t.Errorf("the queue holds %d tickets, want 0", len(queue))
	}
}

func TestQueueGivesTheSequenceOfPosition(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	projectID, err := s.AddProject("/projects/path", "main")
	if err != nil {
		t.Fatal(err)
	}

	// The position of each ticket runs against its id. A queue in the sequence
	// of id therefore looks different from a queue in the sequence of position,
	// and the test can tell the two apart.
	for i, title := range []string{"first", "second", "third"} {
		position := 3 - i
		id, err := s.AddTicket(projectID, title)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.Exec("UPDATE tickets SET position = ? WHERE id = ?", position, id); err != nil {
			t.Fatal(err)
		}
	}

	queue, err := s.Queue()
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"third", "second", "first"}
	if len(queue) != len(want) {
		t.Fatalf("the queue holds %d tickets, want %d", len(queue), len(want))
	}
	for i, title := range want {
		if queue[i].Title != title {
			t.Errorf("ticket %d is %q, want %q", i, queue[i].Title, title)
		}
	}
}

func TestOpenStopsTwoTicketsFromSharingAPosition(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	projectID, err := s.AddProject("/projects/path", "main")
	if err != nil {
		t.Fatal(err)
	}
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

// The database of a person who has the version before this one holds the first
// step and not the second. This is the path that such a database takes.
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

	if got := userVersion(t, second.db); got != 2 {
		t.Errorf("user_version = %d, want 2", got)
	}
	var name string
	err = second.db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type = 'index' AND name = 'tickets_position'").Scan(&name)
	if err != nil {
		t.Errorf("the index of the second step is not on the old database: %v", err)
	}
}
