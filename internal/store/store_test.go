// These tests call Open directly rather than through a helper like the
// openStore in the cli and run tests. Open is the thing under test here: the
// migration steps, the refusal of a database from a later version, the WAL
// mode and the permission of the file are each a behaviour of Open, and a
// helper that stopped the test on Open's error would hide the very error
// some of these tests wait for.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/alcubie/delegator/internal/config"
)

// emptyStore returns a store that holds one project and no ticket, with the id of
// the project.
func emptyStore(t *testing.T) (*Store, int64) {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

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

// ticketIDs returns the id of each ticket of a list, in the order the list
// holds them.
func ticketIDs(tickets []OpenTicket) []int64 {
	out := make([]int64, 0, len(tickets))
	for _, ticket := range tickets {
		out = append(out, ticket.ID)
	}
	return out
}

// queuedTickets adds one ticket of a project for each title, and returns their
// ids. AddTicket puts each one at the end of the queue, so the order of the
// titles is their order in the queue.
func queuedTickets(t *testing.T, s *Store, projectID int64, titles ...string) []int64 {
	t.Helper()
	ids := make([]int64, 0, len(titles))
	for _, title := range titles {
		id, err := s.AddTicket(projectID, title)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

// threeTickets returns a store that holds one project and three tickets, in the
// order first, second, third, with their ids.
func threeTickets(t *testing.T) (*Store, []int64) {
	t.Helper()
	s, projectID := emptyStore(t)
	return s, queuedTickets(t, s, projectID, "first", "second", "third")
}

// twoProjects returns a store that holds two projects with two tickets each,
// and the ids of the tickets of each project. The queue holds the two tickets
// of the first project and then the two of the second.
func twoProjects(t *testing.T) (*Store, []int64, []int64) {
	t.Helper()
	s, firstProject := emptyStore(t)
	secondProject, err := s.AddProject("/projects/other", "main")
	if err != nil {
		t.Fatal(err)
	}
	return s,
		queuedTickets(t, s, firstProject, "first one", "first two"),
		queuedTickets(t, s, secondProject, "second one", "second two")
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
	titles := make([]string, 0, 6)
	for i := range 6 {
		titles = append(titles, fmt.Sprintf("ticket %d", i))
	}
	return first, second, queuedTickets(t, first, projectID, titles...)
}

// claimBranch is the branch of a claim in these tests. ClaimNext takes a
// function and not a name, because only the transaction knows which ticket it
// claimed.
func claimBranch(t Ticket) string {
	return fmt.Sprintf("delegator/%d-%s", t.ID, t.Title)
}

// claimableCount returns how many tickets of the queue a claim could take, and
// stops the test when the read fails.
func claimableCount(t *testing.T, s *Store, cfg config.Config) int {
	t.Helper()
	n, err := s.ClaimableCount(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return n
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

// threeReady returns a store that holds one project and three ready tickets,
// in the order first, second, third, with their ids. Each one goes through
// running, which is the way a ticket reaches READY.
func threeReady(t *testing.T) (*Store, []int64) {
	t.Helper()
	s, ids := threeTickets(t)
	for _, id := range ids {
		if err := s.ChangeStatus(id, Running); err != nil {
			t.Fatal(err)
		}
		if err := s.ChangeStatus(id, Ready); err != nil {
			t.Fatal(err)
		}
	}
	return s, ids
}

// readyTitles returns the title of each ready ticket, in the order of READY.
// The store has no method for READY: internal/inbox puts that group in order,
// and this reads the column that it orders by.
func readyTitles(t *testing.T, s *Store) []string {
	t.Helper()
	rows, err := s.db.Query(`
		SELECT title FROM tickets
		WHERE status = ? AND ready_position IS NOT NULL
		ORDER BY ready_position`, Ready)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var titles []string
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			t.Fatal(err)
		}
		titles = append(titles, title)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return titles
}

// ticketPosition returns the place of one ticket in the list that column keeps,
// which is position for the queue and ready_position for READY. A ticket that
// is not in that list holds no place, and the value is then not valid.
func ticketPosition(t *testing.T, s *Store, column string, id int64) sql.Null[int] {
	t.Helper()
	var position sql.Null[int]
	if err := s.db.QueryRow(fmt.Sprintf(
		"SELECT %s FROM tickets WHERE id = ?", column), id).Scan(&position); err != nil {
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

// columnsOf returns the name of each column of one table, in the order of the
// table.
func columnsOf(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return names
}

// runRow is one row of the table runs as a test reads it, with each column
// that can hold NULL as a sql.Null.
type runRow struct {
	id        int64
	pid       sql.Null[int]
	startedAt string
	endedAt   sql.NullString
	exitCode  sql.Null[int]
}

// runRows returns each row of runs that belongs to one ticket, in the order of
// id.
func runRows(t *testing.T, s *Store, ticketID int64) []runRow {
	t.Helper()
	rows, err := s.db.Query(
		"SELECT id, pid, started_at, ended_at, exit_code FROM runs WHERE ticket_id = ? ORDER BY id",
		ticketID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var runs []runRow
	for rows.Next() {
		var r runRow
		if err := rows.Scan(&r.id, &r.pid, &r.startedAt, &r.endedAt, &r.exitCode); err != nil {
			t.Fatal(err)
		}
		runs = append(runs, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return runs
}

// allRunning and noneRunning are the two answers a test gives Reconcile about
// the runs it finds.
func allRunning(Run) bool { return true }

func noneRunning(Run) bool { return false }

// askedRuns returns an answer for Reconcile that keeps each run it was asked
// about, in the order of the asking, and says that every one is running.
func askedRuns(asked *[]Run) func(Run) bool {
	return func(r Run) bool {
		*asked = append(*asked, r)
		return true
	}
}

// setEndedAt writes ended_at on the last run of a ticket, so that a test can
// tell an end time that was there already from one that a call has just
// written. Both would hold the same second otherwise.
func setEndedAt(t *testing.T, s *Store, ticketID int64, ended string) {
	t.Helper()
	if _, err := s.db.Exec(`
		UPDATE runs SET ended_at = ?
		WHERE id = (SELECT id FROM runs WHERE ticket_id = ? ORDER BY id DESC LIMIT 1)`,
		ended, ticketID); err != nil {
		t.Fatal(err)
	}
}

// setMigrations puts a different list of steps in place for one test. Each test
// of this package runs one after the other, so no test sees the list of another.
func setMigrations(t *testing.T, list []string) {
	t.Helper()
	old := migrations
	migrations = list
	t.Cleanup(func() { migrations = old })
}

// openBefore opens the database below dataDir as the version of delegator that
// came before the step: the list of migrations is cut at the step for the call
// and put back before it returns, so the next Open is the one under test.
//
// It names the step rather than counting from the end of the list, because a
// step added to the end would otherwise change which database each test that
// counts gets.
func openBefore(t *testing.T, dataDir, step string) *Store {
	t.Helper()
	i := slices.Index(migrations, step)
	if i < 0 {
		t.Fatal("the step is not one of the migrations")
	}
	all := migrations
	migrations = all[:i]
	defer func() { migrations = all }()

	s, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	return s
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

	for _, name := range []string{"projects", "tickets", "agents"} {
		var got string
		err := s.db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", name).Scan(&got)
		if err != nil {
			t.Errorf("table %s: %v", name, err)
		}
	}
}

// Section 7 of docs/RUN_CONTROL.md gives the table runs. The process id, the
// start time, the end time and the exit code are facts of a run, and a ticket
// has more than one run after dg restart and dg revise, so a column of tickets
// cannot hold them.
func TestOpenMakesTheTableRunsWithItsColumns(t *testing.T) {
	s, _ := emptyStore(t)

	want := []string{"id", "ticket_id", "pid", "started_at", "ended_at", "exit_code", "agent_id"}
	if got := columnsOf(t, s.db, "runs"); !slices.Equal(got, want) {
		t.Errorf("the columns of runs = %v, want %v", got, want)
	}
}

func TestOpenSeedsTheBuiltInAgents(t *testing.T) {
	s, _ := emptyStore(t)
	rows, err := s.db.Query("SELECT name FROM agents ORDER BY name")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"claude", "codex", "cursor", "gemini", "github-copilot", "goose", "opencode", "pi"}; !slices.Equal(got, want) {
		t.Errorf("seeded agents = %v, want %v", got, want)
	}
	opencode, err := s.Agent("opencode")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"opencode", "acp"}; !slices.Equal(opencode.Argv, want) {
		t.Errorf("opencode argv = %v, want %v", opencode.Argv, want)
	}
	if want := []string{"opencode", "--session", "{session}"}; !slices.Equal(opencode.Resume, want) {
		t.Errorf("opencode resume argv = %v, want %v", opencode.Resume, want)
	}
	if opencode.InstallHint == "" {
		t.Error("opencode has no installation hint")
	}
	goose, err := s.Agent("goose")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"goose", "acp"}; !slices.Equal(goose.Argv, want) {
		t.Errorf("goose argv = %v, want %v", goose.Argv, want)
	}
	if want := []string{"goose", "session", "--resume", "--session-id", "{session}"}; !slices.Equal(goose.Resume, want) {
		t.Errorf("goose resume argv = %v, want %v", goose.Resume, want)
	}
	if goose.InstallHint == "" {
		t.Error("goose has no installation hint")
	}
	copilot, err := s.Agent("github-copilot")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"copilot", "--acp"}; !slices.Equal(copilot.Argv, want) {
		t.Errorf("github-copilot argv = %v, want %v", copilot.Argv, want)
	}
	if want := []string{"copilot", "--resume={session}"}; !slices.Equal(copilot.Resume, want) {
		t.Errorf("github-copilot resume argv = %v, want %v", copilot.Resume, want)
	}
	if copilot.InstallHint == "" {
		t.Error("github-copilot has no installation hint")
	}
}

func TestAgentRegistryRoundTripsAndControlsTheDefault(t *testing.T) {
	s, _ := emptyStore(t)
	if got, err := s.DefaultAgent(); err != nil || got != "" {
		t.Fatalf("default agent = %q, %v; want no selection", got, err)
	}
	want := Agent{Name: "local", Argv: []string{"local-acp", "--stdio"}, Resume: []string{"local", "{session}"}, InstallHint: "install local"}
	if err := s.SaveAgent(want); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDefaultAgent("local"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Agent("local")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Argv, want.Argv) || !slices.Equal(got.Resume, want.Resume) || got.InstallHint != want.InstallHint {
		t.Errorf("agent = %+v, want %+v", got, want)
	}
}

func TestSaveAgentValidatesItsLaunchCommand(t *testing.T) {
	s, _ := emptyStore(t)
	if err := s.SaveAgent(Agent{Name: "broken"}); !errors.Is(err, ErrInvalidAgent) {
		t.Fatalf("SaveAgent error = %v, want ErrInvalidAgent", err)
	}
}

func TestAgentIDRefusesAnAgentOutsideTheRegistry(t *testing.T) {
	s, _ := emptyStore(t)
	if _, err := s.AgentID("not-registered"); !errors.Is(err, ErrInvalidAgent) {
		t.Fatalf("AgentID error = %v, want ErrInvalidAgent", err)
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

func TestDataDirIsTheDirectoryTheStoreWasOpenedIn(t *testing.T) {
	dataDir := t.TempDir()
	s, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if got := s.DataDir(); got != dataDir {
		t.Errorf("DataDir() = %q, want %q", got, dataDir)
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
	}

	var ticket Ticket
	row := s.db.QueryRow("SELECT id, project_id, title, status FROM tickets WHERE id = ?", ticketID)
	if err = row.Scan(
		&ticket.ID,
		&ticket.ProjectID,
		&ticket.Title,
		&ticket.State,
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

// READY had no order of its own before the column ready_position, and the
// inbox put it in the order of the time of completion. The step that adds the
// column gives each ready ticket the place that order had, so the READY of a
// person who upgrades is the one they last looked at.
func TestOpenGivesAnOldReadyTicketThePlaceOfItsCompletion(t *testing.T) {
	dataDir := t.TempDir()

	before := openBefore(t, dataDir, addReadyPositionColumn)
	projectID, err := before.AddProject("/projects/path", "main")
	if err != nil {
		t.Fatal(err)
	}
	// The tickets go in by hand: this version of the store writes the column
	// that the old database does not have yet.
	for _, ticket := range []struct {
		title     string
		completed string
	}{
		{"last", "2026-08-28T15:00:00Z"},
		{"first", "2026-08-28T09:00:00Z"},
		{"middle", "2026-08-28T12:00:00Z"},
	} {
		if _, err := before.db.Exec(`
			INSERT INTO tickets (project_id, title, status, created, completed)
			VALUES (?, ?, 'ready', '2026-08-28T08:00:00Z', ?)`,
			projectID, ticket.title, ticket.completed); err != nil {
			t.Fatal(err)
		}
	}
	before.Close()

	s, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	want := []string{"first", "middle", "last"}
	if got := readyTitles(t, s); !slices.Equal(got, want) {
		t.Errorf("READY is %v, want %v", got, want)
	}
}

// The database of a person who has an earlier version of delegator holds only
// the steps of that version. The next start must apply each step that is
// missing, and no step that is present. This is the path that such a database
// takes.
func TestOpenAppliesANewStepToAnOldDatabase(t *testing.T) {
	dataDir := t.TempDir()

	first := openBefore(t, dataDir, completedColumn)
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
	if position := ticketPosition(t, s, "position", ids[0]); position.V != 1 {
		t.Errorf("the first ticket is at position %d, want 1", position.V)
	}
}

func TestChangeStatusOutOfTheQueueLeavesTheOthersInOrder(t *testing.T) {
	s, ids := threeTickets(t)

	if err := s.ChangeStatus(ids[0], Running); err != nil {
		t.Fatal(err)
	}

	want := []string{"second", "third"}
	if got := queueTitles(t, s); !slices.Equal(got, want) {
		t.Errorf("the queue is %v, want %v", got, want)
	}
}

func TestChangeStatusFromQueuedToAStatusItCannotReach(t *testing.T) {
	s, id := oneTicket(t)

	for _, state := range []TicketStatus{Ready, Failed, Done, Queued} {
		if err := s.ChangeStatus(id, state); !errors.Is(err, ErrInvalidTicketStateChange) {
			t.Errorf("ChangeStatus to %s gave err = %v, want ErrInvalidTicketStateChange", state, err)
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

func TestMoveTicketWritesTheNewOrder(t *testing.T) {
	s, ids := threeTickets(t)

	if err := s.MoveTicket(ids[2], Top); err != nil {
		t.Fatal(err)
	}

	want := []string{"third", "first", "second"}
	if got := queueTitles(t, s); !slices.Equal(got, want) {
		t.Errorf("the queue is %v, want %v", got, want)
	}

	if position := ticketPosition(t, s, "position", ids[2]); position.V != 1 {
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

func TestMoveTicketThatIsInNoListChangesNothing(t *testing.T) {
	s, ids := threeTickets(t)
	if err := s.ChangeStatus(ids[1], Running); err != nil {
		t.Fatal(err)
	}
	before := queueTitles(t, s)

	err := s.MoveTicket(ids[1], Top)
	if !errors.Is(err, ErrNotMovable) {
		t.Errorf("err = %v, want ErrNotMovable", err)
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

// setAccepted writes the time that the person accepted a ticket, which is a
// row of its history. dg accept does this work, and a test that is not about
// dg accept writes the row rather than the whole path of a run and a review.
func setAccepted(t *testing.T, s *Store, id int64, accepted string) {
	t.Helper()
	if _, err := s.db.Exec(`
		INSERT INTO transitions (ticket_id, from_status, to_status, at)
		VALUES (?, 'ready', 'done', ?)`, id, accepted); err != nil {
		t.Fatal(err)
	}
}

// setReady writes the time that a run of a ticket finished, which is a row of
// its history. No time of the inbox comes from it, and a test writes it to say
// which row the inbox reads.
func setReady(t *testing.T, s *Store, id int64, ready string) {
	t.Helper()
	if _, err := s.db.Exec(`
		INSERT INTO transitions (ticket_id, from_status, to_status, at)
		VALUES (?, 'running', 'ready', ?)`, id, ready); err != nil {
		t.Fatal(err)
	}
}

// A ticket of the open list holds no time of acceptance, whatever its history
// holds. A ready ticket has finished a run, and the time of that run is not an
// acceptance: the person has not read the work yet.
func TestOpenTicketsHoldNoTimeOfAcceptance(t *testing.T) {
	s, ids := threeTickets(t)
	setStatus(t, s, ids[0], Ready)
	setReady(t, s, ids[0], "2026-08-28T09:30:00Z")

	open, err := s.OpenTickets()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[int64]OpenTicket{}
	for _, ticket := range open {
		byID[ticket.ID] = ticket
	}
	if got := byID[ids[0]].Accepted; !got.IsZero() {
		t.Errorf("a ready ticket has accepted = %v, want the zero time", got)
	}
	if got := byID[ids[2]].Accepted; !got.IsZero() {
		t.Errorf("a queued ticket has accepted = %v, want the zero time", got)
	}
}

// The inbox shows how long a ticket has been waiting, so each row of it holds
// the time the ticket arrived. That is the first row of its history, which
// every ticket has.
func TestOpenTicketsHoldTheTimeOfCreation(t *testing.T) {
	s, ids := threeTickets(t)
	before := time.Now().Add(-time.Second)

	open, err := s.OpenTickets()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[int64]OpenTicket{}
	for _, ticket := range open {
		byID[ticket.ID] = ticket
	}
	for _, id := range ids {
		got := byID[id].Created
		if got.Before(before) || got.After(time.Now()) {
			t.Errorf("the ticket %d was created at %v, want between %v and now", id, got, before)
		}
	}
}

// setStarted writes the start time of one run. Claim writes the time of the
// call, to the second, so two claims in one test hold the same text and only a
// write like this one tells them apart.
func setStarted(t *testing.T, s *Store, runID int64, started string) {
	t.Helper()
	if _, err := s.db.Exec(
		"UPDATE runs SET started_at = ? WHERE id = ?", started, runID); err != nil {
		t.Fatal(err)
	}
}

// The inbox shows how long a run has been going, so each open ticket carries
// the start of its run. A ticket that no supervisor has claimed has no run,
// and holds the zero time.
func TestOpenTicketsGivesTheStartOfTheRun(t *testing.T) {
	s, ids := threeTickets(t)
	if _, err := s.Claim(ids[0], "delegator/1-first"); err != nil {
		t.Fatal(err)
	}
	setStarted(t, s, runRows(t, s, ids[0])[0].id, "2026-08-28T09:30:00Z")

	open, err := s.OpenTickets()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[int64]OpenTicket{}
	for _, ticket := range open {
		byID[ticket.ID] = ticket
	}
	want := time.Date(2026, 8, 28, 9, 30, 0, 0, time.UTC)
	if got := byID[ids[0]].Started; !got.Equal(want) {
		t.Errorf("started = %v, want %v", got, want)
	}
	if got := byID[ids[2]].Started; !got.IsZero() {
		t.Errorf("a queued ticket that never ran has started = %v, want the zero time", got)
	}
}

// A ticket that failed and restarted has a run for each claim,
// and the run that holds it now is the last one. The time of an earlier run
// would give the inbox a duration of hours for a run of a minute.
func TestOpenTicketsGivesTheStartOfTheLastRun(t *testing.T) {
	s, id := oneTicket(t)
	if _, err := s.Claim(id, "delegator/1-my-ticket"); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeStatus(id, Failed); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Restart(id); err != nil {
		t.Fatal(err)
	}
	rows := runRows(t, s, id)
	if len(rows) != 2 {
		t.Fatalf("runs = %+v, want two", rows)
	}
	setStarted(t, s, rows[0].id, "2026-08-28T09:00:00Z")
	setStarted(t, s, rows[1].id, "2026-08-28T11:00:00Z")

	open, err := s.OpenTickets()
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 {
		t.Fatalf("OpenTickets gives %d tickets, want 1", len(open))
	}
	want := time.Date(2026, 8, 28, 11, 0, 0, 0, time.UTC)
	if got := open[0].Started; !got.Equal(want) {
		t.Errorf("started = %v, want %v, the start of the second claim", got, want)
	}
}

// setFailed writes the time that a run of a ticket failed, which is a row of
// its history. dg fail writes this row, and a test writes it to say which time
// the inbox reads.
func setFailed(t *testing.T, s *Store, id int64, failed string) {
	t.Helper()
	if _, err := s.db.Exec(`
		INSERT INTO transitions (ticket_id, from_status, to_status, at)
		VALUES (?, 'running', 'failed', ?)`, id, failed); err != nil {
		t.Fatal(err)
	}
}

// A ticket that failed is open: the person must see it in the inbox, and it
// carries the time that it entered failed, which is the time of the failure.
// The group FAILED comes in the order of that time.
func TestOpenTicketsGivesAFailedTicketWithTheTimeOfTheFailure(t *testing.T) {
	s, ids := threeTickets(t)
	setStatus(t, s, ids[0], Failed)
	setFailed(t, s, ids[0], "2026-08-28T09:30:00Z")

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
	if got := byID[ids[0]].Status; got != Failed {
		t.Errorf("ticket %d has the status %q, want failed", ids[0], got)
	}
	want := time.Date(2026, 8, 28, 9, 30, 0, 0, time.UTC)
	if got := byID[ids[0]].Changed; !got.Equal(want) {
		t.Errorf("changed = %v, want %v, the time of the failure", got, want)
	}
}

func TestTicketReturnsEachFieldOfOneRow(t *testing.T) {
	s, ids := threeTickets(t)
	setStatus(t, s, ids[1], Ready)
	setReady(t, s, ids[1], "2026-08-28T09:30:00Z")
	if _, err := s.db.Exec(
		`UPDATE tickets SET branch = ?, session = ? WHERE id = ?`,
		"delegator/2-second", "a-session-id", ids[1]); err != nil {
		t.Fatal(err)
	}

	got, err := s.Ticket(ids[1])
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, got, want string }{
		{"project", got.Project.Path, "/projects/path"},
		{"title", got.Title, "second"},
		{"status", string(got.Status), "ready"},
		{"branch", got.Branch, "delegator/2-second"},
		{"session", got.Session, "a-session-id"},
	} {
		if test.got != test.want {
			t.Errorf("%s = %q, want %q", test.name, test.got, test.want)
		}
	}
	if want := time.Date(2026, 8, 28, 9, 30, 0, 0, time.UTC); !got.Changed.Equal(want) {
		t.Errorf("the ticket changed at %v, want %v", got.Changed, want)
	}
	if got.ID != ids[1] {
		t.Errorf("id = %d, want %d", got.ID, ids[1])
	}
}

// A ticket that is not in the queue holds no position, and a column that holds
// NULL arrives as the zero value of its type.
func TestTicketWithNoPositionAndNoBranch(t *testing.T) {
	s, id := oneTicket(t)
	if err := s.ChangeStatus(id, Running); err != nil {
		t.Fatal(err)
	}

	got, err := s.Ticket(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Position != 0 {
		t.Errorf("position = %d, want 0", got.Position)
	}
	if got.Branch != "" || got.Session != "" {
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
	if err := s.ChangeStatus(ids[2], Running); err != nil {
		t.Fatal(err)
	}
	before := queueTitles(t, s)

	err := s.MoveTicketBefore(ids[0], ids[2])
	if !errors.Is(err, ErrNotInTheSameList) {
		t.Errorf("err = %v, want ErrNotInTheSameList", err)
	}
	if got := queueTitles(t, s); !slices.Equal(got, before) {
		t.Errorf("the queue is %v, want %v", got, before)
	}
}

func TestMoveTicketBeforeWithATicketThatIsInNoList(t *testing.T) {
	s, ids := threeTickets(t)
	if err := s.ChangeStatus(ids[0], Running); err != nil {
		t.Fatal(err)
	}

	if err := s.MoveTicketBefore(ids[0], ids[2]); !errors.Is(err, ErrNotMovable) {
		t.Errorf("err = %v, want ErrNotMovable", err)
	}
}

// A person who wants to review a later ticket first moves it inside READY, and
// the tickets of the queue below it do not move.
func TestMoveTicketMovesInEachDirectionInReady(t *testing.T) {
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
			s, ids := threeReady(t)
			if err := s.MoveTicket(ids[1], test.move); err != nil {
				t.Fatal(err)
			}
			if got := readyTitles(t, s); !slices.Equal(got, test.want) {
				t.Errorf("READY is %v, want %v", got, test.want)
			}
		})
	}
}

func TestMoveTicketBeforeInReady(t *testing.T) {
	s, ids := threeReady(t)

	if err := s.MoveTicketBefore(ids[2], ids[0]); err != nil {
		t.Fatal(err)
	}
	want := []string{"third", "first", "second"}
	if got := readyTitles(t, s); !slices.Equal(got, want) {
		t.Errorf("READY is %v, want %v", got, want)
	}
}

// A move orders one list. A ready ticket therefore cannot take the place of a
// ticket of the queue, which would put it among the tickets that wait for a
// run.
func TestMoveTicketBeforeATargetInTheOtherList(t *testing.T) {
	s, projectID := emptyStore(t)
	queued, err := s.AddTicket(projectID, "queued")
	if err != nil {
		t.Fatal(err)
	}
	ready, err := s.AddTicket(projectID, "ready")
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []TicketStatus{Running, Ready} {
		if err := s.ChangeStatus(ready, status); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.MoveTicketBefore(ready, queued); !errors.Is(err, ErrNotInTheSameList) {
		t.Errorf("err = %v, want ErrNotInTheSameList", err)
	}
	if got := queueTitles(t, s); !slices.Equal(got, []string{"queued"}) {
		t.Errorf("the queue is %v, want [queued]", got)
	}
	if got := readyTitles(t, s); !slices.Equal(got, []string{"ready"}) {
		t.Errorf("READY is %v, want [ready]", got)
	}
}

// The two lists keep their own column, so a move in one leaves the other as it
// was.
func TestMoveTicketInReadyLeavesTheQueue(t *testing.T) {
	s, ids := threeTickets(t)
	for _, status := range []TicketStatus{Running, Ready} {
		if err := s.ChangeStatus(ids[0], status); err != nil {
			t.Fatal(err)
		}
	}
	for _, status := range []TicketStatus{Running, Ready} {
		if err := s.ChangeStatus(ids[1], status); err != nil {
			t.Fatal(err)
		}
	}
	before := queueTitles(t, s)

	if err := s.MoveTicket(ids[1], Top); err != nil {
		t.Fatal(err)
	}
	if got := readyTitles(t, s); !slices.Equal(got, []string{"second", "first"}) {
		t.Errorf("READY is %v, want [second first]", got)
	}
	if got := queueTitles(t, s); !slices.Equal(got, before) {
		t.Errorf("the queue is %v, want %v", got, before)
	}
}

// nextStates says which change one status can take, and ChangeStatus obeys it.
func TestChangeStatusWithAChangeThatNextStatesDoesNotHold(t *testing.T) {
	s, id := oneTicket(t)
	if err := s.ChangeStatus(id, Running); err != nil {
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
	if err := s.ChangeStatus(id, Cancelled); err != nil {
		t.Fatal(err)
	}

	if err := s.ChangeStatus(id, Running); !errors.Is(err, ErrInvalidTicketStateChange) {
		t.Errorf("err = %v, want ErrInvalidTicketStateChange", err)
	}
}

// A ticket that becomes ready goes to the end of READY, which is where the
// order of completion put it before READY had an order of its own.
func TestChangeStatusIntoReadyPutsTheTicketAtTheEnd(t *testing.T) {
	s, ids := threeReady(t)
	if err := s.MoveTicket(ids[2], Top); err != nil {
		t.Fatal(err)
	}

	fourth, err := s.AddTicket(mustProject(t, s), "fourth")
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []TicketStatus{Running, Ready} {
		if err := s.ChangeStatus(fourth, status); err != nil {
			t.Fatal(err)
		}
	}

	want := []string{"third", "first", "second", "fourth"}
	if got := readyTitles(t, s); !slices.Equal(got, want) {
		t.Errorf("READY is %v, want %v", got, want)
	}
}

// dg accept and dg revise take a ticket out of READY, and a ticket that is not
// in a list keeps no place in it. No CHECK holds this the way one holds the
// position of the queue, and a place left behind comes back as a number that
// another ready ticket wants.
func TestChangeStatusOutOfReadyClearsTheReadyPosition(t *testing.T) {
	for _, status := range []TicketStatus{Done, Queued} {
		t.Run(string(status), func(t *testing.T) {
			s, ids := threeReady(t)

			if err := s.ChangeStatus(ids[1], status); err != nil {
				t.Fatal(err)
			}

			if position := ticketPosition(t, s, "ready_position", ids[1]); position.Valid {
				t.Errorf("ready_position = %d, want none", position.V)
			}
			if got := readyTitles(t, s); !slices.Equal(got, []string{"first", "third"}) {
				t.Errorf("READY is %v, want [first third]", got)
			}
		})
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

func TestChangeStatusOnATicketThatIsNotThere(t *testing.T) {
	s, _ := emptyStore(t)

	if err := s.ChangeStatus(9999, Running); !errors.Is(err, ErrNoTicket) {
		t.Errorf("err = %v, want ErrNoTicket", err)
	}
}

// The supervisor claims a ticket, so the process id of the caller of Claim is
// the process id that the reconcile asks about. The start time is the moment
// of the claim, in RFC 3339 and UTC like each other time of the store.
func TestClaimWritesARunWithThePidAndTheStartTime(t *testing.T) {
	s, id := oneTicket(t)
	before := time.Now().UTC().Truncate(time.Second)

	if _, err := s.Claim(id, "delegator/1-my-ticket"); err != nil {
		t.Fatal(err)
	}

	runs := runRows(t, s, id)
	if len(runs) != 1 {
		t.Fatalf("runs = %+v, want one", runs)
	}
	if got := runs[0].pid; !got.Valid || got.V != os.Getpid() {
		t.Errorf("pid = %+v, want %d", got, os.Getpid())
	}
	started, err := time.Parse(time.RFC3339, runs[0].startedAt)
	if err != nil {
		t.Fatalf("started_at = %q, want RFC 3339: %v", runs[0].startedAt, err)
	}
	if started.Before(before) || started.After(time.Now()) {
		t.Errorf("started_at = %s, want between %s and now", started, before)
	}
	if runs[0].endedAt.Valid || runs[0].exitCode.Valid {
		t.Errorf("a run that has not ended holds ended_at = %+v, exit_code = %+v", runs[0].endedAt, runs[0].exitCode)
	}
}

// Two supervisors can reach one ticket, and the one that loses writes nothing:
// the row of runs is below the transaction of the change of state, so a claim
// that the state machine refuses leaves no run behind.
func TestClaimThatIsRefusedWritesNoRun(t *testing.T) {
	s, id := oneTicket(t)
	if _, err := s.Claim(id, "delegator/1-my-ticket"); err != nil {
		t.Fatal(err)
	}

	runID, err := s.Claim(id, "delegator/1-my-ticket")
	if !errors.Is(err, ErrInvalidTicketStateChange) {
		t.Fatalf("err = %v, want ErrInvalidTicketStateChange", err)
	}
	if runID != 0 {
		t.Errorf("run id = %d, want 0 from a claim that wrote nothing", runID)
	}

	if runs := runRows(t, s, id); len(runs) != 1 {
		t.Errorf("runs = %+v, want the one of the first claim", runs)
	}
}

// The supervisor claims its own ticket, so the read of the queue and the write
// of the claim are one transaction. What it claims is the first ticket of the
// queue, which is what the trigger used to read for it.
func TestClaimNextTakesTheFirstTicketOfTheQueue(t *testing.T) {
	s, ids := threeTickets(t)
	// The queue is in the order of position and not of id, so the ticket the
	// person moved to the top is the one the supervisor takes.
	if err := s.MoveTicket(ids[2], Top); err != nil {
		t.Fatal(err)
	}

	claimed, runID, err := s.ClaimNext(config.Config{Runs: 1}, claimBranch)
	if err != nil {
		t.Fatal(err)
	}

	if claimed.ID != ids[2] {
		t.Errorf("claimed ticket %d, want the first of the queue %d", claimed.ID, ids[2])
	}
	if claimed.Status != Running {
		t.Errorf("status = %q, want %q", claimed.Status, Running)
	}
	want := claimBranch(claimed)
	if claimed.Branch != want {
		t.Errorf("branch = %q, want %q", claimed.Branch, want)
	}

	stored, err := s.Ticket(ids[2])
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != Running || stored.Branch != want {
		t.Errorf("the row holds status %q and branch %q, want %q and %q",
			stored.Status, stored.Branch, Running, want)
	}
	runs := runRows(t, s, ids[2])
	if len(runs) != 1 {
		t.Fatalf("runs = %+v, want one", runs)
	}
	if runs[0].id != runID {
		t.Errorf("the claim gave the run id %d, want %d", runID, runs[0].id)
	}
	if got := runs[0].pid; !got.Valid || got.V != os.Getpid() {
		t.Errorf("pid = %+v, want %d", got, os.Getpid())
	}
}

// A supervisor that finds nothing to claim stops, and the queue with no ticket
// at all is the plainest way to find nothing.
func TestClaimNextWithAnEmptyQueueClaimsNothing(t *testing.T) {
	s, _ := emptyStore(t)

	claimed, runID, err := s.ClaimNext(config.Config{Runs: 1}, claimBranch)

	if !errors.Is(err, ErrNoRoom) {
		t.Fatalf("err = %v, want ErrNoRoom", err)
	}
	if claimed.ID != 0 || runID != 0 {
		t.Errorf("claimed ticket %d as run %d, want nothing", claimed.ID, runID)
	}
}

// With a limit of one, one ticket fills the queue however long the queue
// behind it, and the ticket in ready is work the person has not examined yet,
// which fills it in the same way.
func TestClaimNextWithATicketThatHoldsTheQueue(t *testing.T) {
	for _, status := range []TicketStatus{Running, Ready} {
		s, ids := threeTickets(t)
		if _, err := s.Claim(ids[0], "delegator/1-first"); err != nil {
			t.Fatal(err)
		}
		if status == Ready {
			if err := s.ChangeStatus(ids[0], Ready); err != nil {
				t.Fatal(err)
			}
		}

		claimed, _, err := s.ClaimNext(config.Config{Runs: 1}, claimBranch)

		if !errors.Is(err, ErrNoRoom) {
			t.Errorf("with a ticket in %s: err = %v, want ErrNoRoom", status, err)
		}
		if claimed.ID != 0 {
			t.Errorf("with a ticket in %s: claimed ticket %d, want nothing", status, claimed.ID)
		}
	}
}

// A limit above one leaves a slot for a second supervisor, which claims the
// ticket below the one that is running: two runs, in two worktrees, at one
// time.
func TestClaimNextWithASlotFreeClaimsTheNextTicket(t *testing.T) {
	s, ids := threeTickets(t)
	if _, err := s.Claim(ids[0], "delegator/1-first"); err != nil {
		t.Fatal(err)
	}

	claimed, _, err := s.ClaimNext(config.Config{Runs: 2}, claimBranch)

	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != ids[1] {
		t.Errorf("claimed ticket %d, want the first ticket still queued %d", claimed.ID, ids[1])
	}
}

// A ticket in ready holds its slot, at any limit: the person has not examined
// that work yet, and the count is of the tickets a run has opened and nobody
// has closed.
func TestClaimNextWithEverySlotFullClaimsNothing(t *testing.T) {
	s, ids := threeTickets(t)
	for _, id := range ids[:2] {
		if _, err := s.Claim(id, claimBranch(Ticket{ID: id})); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ChangeStatus(ids[0], Ready); err != nil {
		t.Fatal(err)
	}

	claimed, _, err := s.ClaimNext(config.Config{Runs: 2}, claimBranch)

	if !errors.Is(err, ErrNoRoom) {
		t.Fatalf("err = %v, want ErrNoRoom", err)
	}
	if claimed.ID != 0 {
		t.Errorf("claimed ticket %d with both slots full, want nothing", claimed.ID)
	}
}

// A person who lowers the limit while runs are going has more tickets open
// than the limit allows. The claim takes nothing until they close enough of
// them, and the count of slots below zero is a count of none.
func TestClaimNextWithMoreTicketsOpenThanTheLimitClaimsNothing(t *testing.T) {
	s, ids := threeTickets(t)
	for _, id := range ids[:2] {
		if _, err := s.Claim(id, claimBranch(Ticket{ID: id})); err != nil {
			t.Fatal(err)
		}
	}

	claimed, _, err := s.ClaimNext(config.Config{Runs: 1}, claimBranch)

	if !errors.Is(err, ErrNoRoom) {
		t.Fatalf("err = %v, want ErrNoRoom", err)
	}
	if claimed.ID != 0 {
		t.Errorf("claimed ticket %d with the limit below the tickets open, want nothing", claimed.ID)
	}
}

// A paused queue starts no run of its own. dg run with an id still claims,
// because a person typed it, and this is the claim that no person asked for.
func TestClaimNextWithAPausedQueueClaimsNothing(t *testing.T) {
	s, _ := threeTickets(t)
	if err := s.PauseQueue(); err != nil {
		t.Fatal(err)
	}

	claimed, _, err := s.ClaimNext(config.Config{Runs: 1}, claimBranch)

	if !errors.Is(err, ErrNoRoom) {
		t.Fatalf("err = %v, want ErrNoRoom", err)
	}
	if claimed.ID != 0 {
		t.Errorf("claimed ticket %d on a paused queue, want nothing", claimed.ID)
	}
}

// A project at its limit does not stop the queue. The claim passes over the
// ticket that waits behind it and takes the first ticket below that one whose
// project has room, so the first ticket of the queue is not always the next to
// run.
func TestClaimNextPassesOverAProjectAtItsLimit(t *testing.T) {
	s, first, second := twoProjects(t)
	if _, err := s.Claim(first[0], claimBranch(Ticket{ID: first[0]})); err != nil {
		t.Fatal(err)
	}

	claimed, _, err := s.ClaimNext(config.Config{Runs: 3, MaxRunsPerProject: 1}, claimBranch)

	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != second[0] {
		t.Errorf("claimed ticket %d, want the first ticket of the other project %d",
			claimed.ID, second[0])
	}
}

// A limit above one leaves a project room for a second ticket of its own, and
// the claim then takes the first ticket of the queue as it does with no limit
// for each project at all.
func TestClaimNextTakesASecondTicketOfAProjectWithRoom(t *testing.T) {
	s, first, _ := twoProjects(t)
	if _, err := s.Claim(first[0], claimBranch(Ticket{ID: first[0]})); err != nil {
		t.Fatal(err)
	}

	claimed, _, err := s.ClaimNext(config.Config{Runs: 3, MaxRunsPerProject: 2}, claimBranch)

	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != first[1] {
		t.Errorf("claimed ticket %d, want the second ticket of the same project %d",
			claimed.ID, first[1])
	}
}

// The queue has slots free and every ticket left in it belongs to a project
// that is at its limit. The supervisor that a trigger started for one of those
// slots finds nothing and stops.
func TestClaimNextWithEveryProjectAtItsLimitClaimsNothing(t *testing.T) {
	s, first, second := twoProjects(t)
	for _, id := range []int64{first[0], second[0]} {
		if _, err := s.Claim(id, claimBranch(Ticket{ID: id})); err != nil {
			t.Fatal(err)
		}
	}

	claimed, _, err := s.ClaimNext(config.Config{Runs: 4, MaxRunsPerProject: 1}, claimBranch)

	if !errors.Is(err, ErrNoRoom) {
		t.Fatalf("err = %v, want ErrNoRoom", err)
	}
	if claimed.ID != 0 {
		t.Errorf("claimed ticket %d with every project at its limit, want nothing", claimed.ID)
	}
}

// A ticket in ready holds a place of its project for the reason it holds a
// slot of the whole queue: the person has not examined that work yet.
func TestClaimNextCountsAReadyTicketAgainstItsProject(t *testing.T) {
	s, first, second := twoProjects(t)
	if _, err := s.Claim(first[0], claimBranch(Ticket{ID: first[0]})); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeStatus(first[0], Ready); err != nil {
		t.Fatal(err)
	}

	claimed, _, err := s.ClaimNext(config.Config{Runs: 3, MaxRunsPerProject: 1}, claimBranch)

	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != second[0] {
		t.Errorf("claimed ticket %d, want the first ticket of the other project %d",
			claimed.ID, second[0])
	}
}

// A queue of tickets that nothing holds back counts one for each of them, and
// that is what a trigger starts supervisors for.
func TestClaimableCountCountsTheTicketsOfTheQueue(t *testing.T) {
	s, _ := threeTickets(t)

	if got := claimableCount(t, s, config.Config{Runs: 4}); got != 3 {
		t.Errorf("count = %d, want 3: every ticket of the queue can be claimed", got)
	}
}

// The count never passes the free slots of the whole queue, whatever room the
// projects have between them. Two projects with three tickets the rule holds
// for, a limit of two for the queue and one run active leaves one slot, and
// one slot is what the supervisors will find.
func TestClaimableCountStopsAtTheFreeSlots(t *testing.T) {
	s, first, _ := twoProjects(t)
	if _, err := s.Claim(first[0], claimBranch(Ticket{ID: first[0]})); err != nil {
		t.Fatal(err)
	}

	got := claimableCount(t, s, config.Config{Runs: 2, MaxRunsPerProject: 2})

	if got != 1 {
		t.Errorf("count = %d with one slot of the queue free, want 1", got)
	}
}

// The fault this count was written for. The queue has slots free and every
// ticket left in it belongs to a project that is at its limit, so every
// supervisor a trigger started would find ErrNoRoom and stop. None starts.
func TestClaimableCountWithEveryProjectAtItsLimitCountsNothing(t *testing.T) {
	s, first, second := twoProjects(t)
	for _, id := range []int64{first[0], second[0]} {
		if _, err := s.Claim(id, claimBranch(Ticket{ID: id})); err != nil {
			t.Fatal(err)
		}
	}

	got := claimableCount(t, s, config.Config{Runs: 4, MaxRunsPerProject: 1})

	if got != 0 {
		t.Errorf("count = %d with two slots free and every project at its limit, want 0", got)
	}
}

// Two tickets of one project can be claimed one at a time, not both: the first
// claim fills the one place the project has. The count is the tickets the
// claims will take and not the tickets the rule holds for right now.
func TestClaimableCountStopsAtTheRoomOfOneProject(t *testing.T) {
	s, _ := threeTickets(t)

	got := claimableCount(t, s, config.Config{Runs: 4, MaxRunsPerProject: 1})

	if got != 1 {
		t.Errorf("count = %d for three tickets of one project with a limit of one, want 1", got)
	}
}

// The limit for each project holds for each project on its own, so a queue of
// two projects with a place each counts one of each.
func TestClaimableCountCountsTheRoomOfEveryProject(t *testing.T) {
	s, _, _ := twoProjects(t)

	got := claimableCount(t, s, config.Config{Runs: 4, MaxRunsPerProject: 1})

	if got != 2 {
		t.Errorf("count = %d for two projects with a place each, want 2", got)
	}
}

// Two triggers at the same time start two supervisors, and both read the same
// queue. The claim is one transaction with the read, so one takes the ticket
// and the other finds nothing.
func TestTwoSupervisorsThatClaimNextTakeOneTicket(t *testing.T) {
	first, second, ids := twoPrograms(t)

	var wg sync.WaitGroup
	claims := make(chan Ticket, 2)
	errs := make(chan error, 2)
	for _, s := range []*Store{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claimed, _, err := s.ClaimNext(config.Config{Runs: 1}, claimBranch)
			claims <- claimed
			errs <- err
		}()
	}
	wg.Wait()
	close(claims)
	close(errs)

	won := 0
	for claimed := range claims {
		if claimed.ID == ids[0] {
			won++
		} else if claimed.ID != 0 {
			t.Errorf("a supervisor claimed ticket %d, want the first of the queue or nothing", claimed.ID)
		}
	}
	if won != 1 {
		t.Errorf("%d supervisors claimed the ticket, want 1", won)
	}
	for err := range errs {
		if err != nil && !errors.Is(err, ErrNoRoom) {
			t.Errorf("err = %v, want nil or ErrNoRoom", err)
		}
	}
	if runs := runRows(t, first, ids[0]); len(runs) != 1 {
		t.Errorf("runs = %+v, want the one of the supervisor that claimed", runs)
	}
}

// Two supervisors that a limit of two started fill the two slots between them:
// each one takes a ticket of its own, and no ticket is claimed twice. The read
// of the queue and the claim are one transaction, so the second supervisor
// reads a queue that no longer holds the first ticket.
func TestTwoSupervisorsWithTwoSlotsTakeTwoTickets(t *testing.T) {
	first, second, ids := twoPrograms(t)

	var wg sync.WaitGroup
	claims := make(chan Ticket, 2)
	for _, s := range []*Store{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claimed, _, err := s.ClaimNext(config.Config{Runs: 2}, claimBranch)
			if err != nil {
				t.Errorf("err = %v, want nil", err)
			}
			claims <- claimed
		}()
	}
	wg.Wait()
	close(claims)

	claimed := make(map[int64]bool)
	for c := range claims {
		claimed[c.ID] = true
	}
	if !claimed[ids[0]] || !claimed[ids[1]] {
		t.Errorf("the supervisors claimed %v, want the first two tickets %d and %d",
			claimed, ids[0], ids[1])
	}
	for _, id := range ids[:2] {
		if runs := runRows(t, first, id); len(runs) != 1 {
			t.Errorf("ticket %d has runs %+v, want the one of the supervisor that claimed it", id, runs)
		}
	}
}

// The reconcile of a ticket in running asks whether its supervisor is alive,
// and it asks with the process id and the start time of the run.
func TestRunGivesThePidAndTheStartTimeOfTheRun(t *testing.T) {
	s, id := oneTicket(t)
	if _, err := s.Claim(id, "delegator/1-my-ticket"); err != nil {
		t.Fatal(err)
	}
	rows := runRows(t, s, id)

	got, err := s.Run(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != rows[0].id {
		t.Errorf("id = %d, want %d", got.ID, rows[0].id)
	}
	if got.TicketID != id {
		t.Errorf("ticket id = %d, want %d", got.TicketID, id)
	}
	if got.PID != os.Getpid() {
		t.Errorf("pid = %d, want %d", got.PID, os.Getpid())
	}
	if got := rfc3339(got.StartedAt); got != rows[0].startedAt {
		t.Errorf("started at = %q, want %q", got, rows[0].startedAt)
	}
}

func TestRunGivesTheAgentItReferences(t *testing.T) {
	s, id := oneTicket(t)
	if err := s.SaveAgent(Agent{Name: "mine", Argv: []string{"mine-acp"}}); err != nil {
		t.Fatal(err)
	}
	runID, err := s.Claim(id, "delegator/1-my-ticket")
	if err != nil {
		t.Fatal(err)
	}
	agentID, err := s.AgentID("mine")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetRunAgent(runID, agentID); err != nil {
		t.Fatal(err)
	}

	run, err := s.Run(id)
	if err != nil {
		t.Fatal(err)
	}
	if run.AgentID != agentID || run.Agent != "mine" {
		t.Errorf("agent = (%d, %q), want (%d, %q)", run.AgentID, run.Agent, agentID, "mine")
	}
}

func TestRunAgentMustReferToAnAgent(t *testing.T) {
	s, id := oneTicket(t)
	runID, err := s.Claim(id, "delegator/1-my-ticket")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.SetRunAgent(runID, 404); err == nil {
		t.Fatal("SetRunAgent accepted an id that no agent has")
	}
}

// A ticket that failed and restarted has a run for each claim,
// and the run of the ticket is the last one: the earlier run ended, and its
// process id belongs to nobody or to a different program by now.
func TestRunGivesTheLastRunOfATicket(t *testing.T) {
	s, id := oneTicket(t)
	if _, err := s.Claim(id, "delegator/1-my-ticket"); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeStatus(id, Failed); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Restart(id); err != nil {
		t.Fatal(err)
	}
	rows := runRows(t, s, id)
	if len(rows) != 2 {
		t.Fatalf("runs = %+v, want two", rows)
	}

	got, err := s.Run(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != rows[1].id {
		t.Errorf("id = %d, want %d, the run of the second claim", got.ID, rows[1].id)
	}
}

// A ticket in the queue has no run yet, and a ticket that is not there has
// none either. Both give ErrNoRun, and the id is in the message.
func TestRunOnATicketThatHasNoRun(t *testing.T) {
	s, id := oneTicket(t)

	for _, ticketID := range []int64{id, 9999} {
		_, err := s.Run(ticketID)
		if !errors.Is(err, ErrNoRun) {
			t.Errorf("ticket %d: err = %v, want ErrNoRun", ticketID, err)
		}
		if err != nil && !strings.Contains(err.Error(), fmt.Sprint(ticketID)) {
			t.Errorf("ticket %d: the message %q does not name the ticket", ticketID, err)
		}
	}
}

// The exit code 0 is a value and not the absence of one, so the column must
// hold it and the store must give it back as a value.
func TestEndRunWritesTheEndTimeAndTheExitCode(t *testing.T) {
	for _, exitCode := range []int{0, 3} {
		s, id := oneTicket(t)
		runID, err := s.Claim(id, "delegator/1-my-ticket")
		if err != nil {
			t.Fatal(err)
		}
		before := time.Now().UTC().Truncate(time.Second)

		if err := s.EndRun(runID, exitCode); err != nil {
			t.Fatal(err)
		}

		rows := runRows(t, s, id)
		if len(rows) != 1 {
			t.Fatalf("runs = %+v, want one", rows)
		}
		if got := rows[0].exitCode; !got.Valid || got.V != exitCode {
			t.Errorf("exit_code = %+v, want %d", got, exitCode)
		}
		if !rows[0].endedAt.Valid {
			t.Fatalf("ended_at is NULL after the run ended")
		}
		ended, err := time.Parse(time.RFC3339, rows[0].endedAt.String)
		if err != nil {
			t.Fatalf("ended_at = %q, want RFC 3339: %v", rows[0].endedAt.String, err)
		}
		if ended.Before(before) || ended.After(time.Now()) {
			t.Errorf("ended_at = %s, want between %s and now", ended, before)
		}

		got, err := s.Run(id)
		if err != nil {
			t.Fatal(err)
		}
		if got := rfc3339(got.EndedAt); got != rows[0].endedAt.String {
			t.Errorf("Run gives ended at %q, want %q", got, rows[0].endedAt.String)
		}
		if !got.ExitCode.Valid || got.ExitCode.V != exitCode {
			t.Errorf("Run gives exit code %+v, want %d", got.ExitCode, exitCode)
		}
	}
}

// The supervisor ends the run it holds, and it names that run. A ticket that
// failed and restarted has a later run that a different supervisor
// holds, and the end of this one must not land on that row.
func TestEndRunEndsTheRunItWasGiven(t *testing.T) {
	s, id := oneTicket(t)
	first, err := s.Claim(id, "delegator/1-my-ticket")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeStatus(id, Failed); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Restart(id); err != nil {
		t.Fatal(err)
	}

	if err := s.EndRun(first, 1); err != nil {
		t.Fatal(err)
	}

	rows := runRows(t, s, id)
	if len(rows) != 2 {
		t.Fatalf("runs = %+v, want two", rows)
	}
	if !rows[0].endedAt.Valid || !rows[0].exitCode.Valid {
		t.Errorf("the first run = %+v, want an end", rows[0])
	}
	if rows[1].endedAt.Valid || rows[1].exitCode.Valid {
		t.Errorf("the second run = %+v, want no end", rows[1])
	}
}

func TestEndRunOnARunThatIsNotThere(t *testing.T) {
	s, _ := oneTicket(t)

	if err := s.EndRun(404, 0); !errors.Is(err, ErrNoRun) {
		t.Errorf("err = %v, want ErrNoRun", err)
	}
}

// A ticket that dg accept closed stays in reach for a while, so the inbox can
// show the person what they finished after it left the open list.
func TestDoneTicketsGivesTheTicketsClosedSinceATime(t *testing.T) {
	s, ids := threeTickets(t)
	for _, id := range ids {
		setStatus(t, s, id, Done)
	}
	setAccepted(t, s, ids[0], "2026-08-27T08:00:00Z")
	setAccepted(t, s, ids[1], "2026-08-28T09:30:00Z")
	setAccepted(t, s, ids[2], "2026-08-28T15:00:00Z")

	done, err := s.DoneTickets(time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{ids[1], ids[2]}; !slices.Equal(ticketIDs(done), want) {
		t.Errorf("DoneTickets gives %v, want %v", ticketIDs(done), want)
	}
}

// A ticket that finished at the moment the window begins is inside it. The
// bound has to fall one way, and a person who asks for the last day means the
// day up to now.
func TestDoneTicketsHoldsATicketAtTheEdgeOfTheWindow(t *testing.T) {
	s, id := oneTicket(t)
	setStatus(t, s, id, Done)
	setAccepted(t, s, id, "2026-08-28T09:00:00Z")

	done, err := s.DoneTickets(time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{id}; !slices.Equal(ticketIDs(done), want) {
		t.Errorf("DoneTickets gives %v, want %v", ticketIDs(done), want)
	}
}

// The window is a time in UTC whatever zone the caller holds, because the
// column is in UTC and the comparison is of the text.
func TestDoneTicketsTakesATimeInAnyZone(t *testing.T) {
	s, id := oneTicket(t)
	setStatus(t, s, id, Done)
	setAccepted(t, s, id, "2026-08-28T09:30:00Z")

	// 06:00 in a zone four hours behind UTC is 10:00 UTC, which is after the
	// ticket. A comparison that took the wall clock of the zone would hold it.
	zone := time.FixedZone("west", -4*60*60)
	done, err := s.DoneTickets(time.Date(2026, 8, 28, 6, 0, 0, 0, zone))
	if err != nil {
		t.Fatal(err)
	}
	if len(done) != 0 {
		t.Errorf("DoneTickets gives %v, want none", ticketIDs(done))
	}
}

// Only a ticket that dg accept closed is in the list. A cancelled ticket was
// thrown away and never finished, and a ticket that is still open is in the
// open list.
func TestDoneTicketsLeavesOutEveryOtherStatus(t *testing.T) {
	s, ids := threeTickets(t)
	setStatus(t, s, ids[0], Cancelled)
	setStatus(t, s, ids[1], Ready)
	setStatus(t, s, ids[2], Done)
	for _, id := range ids {
		setAccepted(t, s, id, "2026-08-28T09:30:00Z")
	}

	done, err := s.DoneTickets(time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{ids[2]}; !slices.Equal(ticketIDs(done), want) {
		t.Errorf("DoneTickets gives %v, want %v", ticketIDs(done), want)
	}
}

// A done ticket whose history holds no change into done is in no window, and it
// does not come out for one that reaches back to the zero time. A version before
// the history left every ticket it had already closed that way.
func TestDoneTicketsLeavesOutATicketWithNoAcceptance(t *testing.T) {
	s, id := oneTicket(t)
	setStatus(t, s, id, Done)

	done, err := s.DoneTickets(time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(done) != 0 {
		t.Errorf("DoneTickets gives %v, want none", ticketIDs(done))
	}
}

// A row of DoneTickets holds the same fields as a row of OpenTickets, because
// the inbox writes the two the same way.
func TestDoneTicketsGivesTheProjectAndTheAcceptance(t *testing.T) {
	s, id := oneTicket(t)
	setStatus(t, s, id, Done)
	setAccepted(t, s, id, "2026-08-28T09:30:00Z")

	done, err := s.DoneTickets(time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(done) != 1 {
		t.Fatalf("DoneTickets gives %v, want the one ticket", ticketIDs(done))
	}
	if got := done[0].Project; got != "/projects/path" {
		t.Errorf("project = %q, want /projects/path", got)
	}
	if got := done[0].Title; got != "My Ticket" {
		t.Errorf("title = %q, want My Ticket", got)
	}
	if got := done[0].Status; got != Done {
		t.Errorf("status = %q, want %q", got, Done)
	}
	want := time.Date(2026, 8, 28, 9, 30, 0, 0, time.UTC)
	if got := done[0].Accepted; !got.Equal(want) {
		t.Errorf("accepted = %v, want %v", got, want)
	}
}

// The window reaches back from the acceptance and not from the end of the run.
// A ticket whose run finished before the window and that the person accepted
// inside it is work they have just dealt with, so DONE holds it.
func TestDoneTicketsMeasuresTheWindowFromTheAcceptance(t *testing.T) {
	s, id := oneTicket(t)
	setStatus(t, s, id, Done)
	setReady(t, s, id, "2026-08-27T08:00:00Z")
	setAccepted(t, s, id, "2026-08-28T09:30:00Z")

	done, err := s.DoneTickets(time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{id}; !slices.Equal(ticketIDs(done), want) {
		t.Errorf("DoneTickets gives %v, want %v", ticketIDs(done), want)
	}
}

// A window of no length holds no ticket, not even one that finished in the
// second the window began in. A person who sets the window to nothing wants no
// DONE at all.
func TestDoneTicketsWithAWindowOfNoLengthHoldsNothing(t *testing.T) {
	s, id := oneTicket(t)
	setStatus(t, s, id, Done)
	setAccepted(t, s, id, "2026-08-28T09:00:00Z")

	// The second is the one the ticket finished in, and the window begins
	// part of the way through it, as a clock in the middle of a second does.
	done, err := s.DoneTickets(time.Date(2026, 8, 28, 9, 0, 0, 500, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(done) != 0 {
		t.Errorf("DoneTickets gives %v, want none", ticketIDs(done))
	}
}

// The supervisor writes the end time and the exit code before it fails the
// ticket, and those are the times of the run itself. A second end time from
// here would move the end of the run to a moment after it.
func TestFailUnfinishedKeepsTheEndThatTheRunHas(t *testing.T) {
	s, id := oneTicket(t)
	runID, err := s.Claim(id, "delegator/1-my-ticket")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EndRun(runID, 2); err != nil {
		t.Fatal(err)
	}
	const ended = "2026-08-28T09:00:00Z"
	setEndedAt(t, s, id, ended)

	if err := s.FailUnfinished(runID); err != nil {
		t.Fatal(err)
	}

	rows := runRows(t, s, id)
	if rows[0].endedAt.String != ended {
		t.Errorf("ended_at = %+v, want %q", rows[0].endedAt, ended)
	}
	if got := rows[0].exitCode; !got.Valid || got.V != 2 {
		t.Errorf("exit_code = %+v, want 2", got)
	}
}

// A run that stopped before it could write its own end still gets one, so no
// run of a ticket that is not running is open.
func TestFailUnfinishedWritesTheEndOfARunThatHasNone(t *testing.T) {
	s, id := oneTicket(t)
	runID, err := s.Claim(id, "delegator/1-my-ticket")
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now().UTC().Truncate(time.Second)

	if err := s.FailUnfinished(runID); err != nil {
		t.Fatal(err)
	}

	run, err := s.Run(id)
	if err != nil {
		t.Fatal(err)
	}
	if run.EndedAt.Before(before) || run.EndedAt.After(time.Now()) {
		t.Errorf("ended at = %s, want between %s and now", run.EndedAt, before)
	}
	if run.ExitCode.Valid {
		t.Errorf("exit code = %+v, want none: nothing collected the program", run.ExitCode)
	}
}

// dg finish made the ticket ready, so the run gave its report and there is
// nothing to fail. A cancelled ticket is the same: the state it has is the one
// the person asked for.
func TestFailUnfinishedLeavesATicketThatIsNotRunning(t *testing.T) {
	for _, status := range []TicketStatus{Ready, Failed, Cancelled} {
		s, id := oneTicket(t)
		runID, err := s.Claim(id, "delegator/1-my-ticket")
		if err != nil {
			t.Fatal(err)
		}
		setStatus(t, s, id, status)

		if err := s.FailUnfinished(runID); err != nil {
			t.Fatalf("%s: %v", status, err)
		}

		ticket, err := s.Ticket(id)
		if err != nil {
			t.Fatal(err)
		}
		if ticket.Status != status {
			t.Errorf("status = %q, want %q", ticket.Status, status)
		}
	}
}

// A supervisor that stopped with no report leaves its ticket in running, and
// only a later command can correct it. Reconcile is that correction: the
// ticket becomes failed, and its run gets the end time it never wrote.
func TestReconcileFailsATicketWhoseRunIsDead(t *testing.T) {
	s, id := oneTicket(t)
	if _, err := s.Claim(id, "delegator/1-my-ticket"); err != nil {
		t.Fatal(err)
	}
	before := time.Now().UTC().Truncate(time.Second)

	marked, err := s.Reconcile(noneRunning)
	if err != nil {
		t.Fatal(err)
	}
	if marked != 1 {
		t.Errorf("Reconcile marked %d tickets, want 1", marked)
	}

	ticket, err := s.Ticket(id)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Status != Failed {
		t.Errorf("status = %q, want %q", ticket.Status, Failed)
	}
	run, err := s.Run(id)
	if err != nil {
		t.Fatal(err)
	}
	if run.EndedAt.Before(before) || run.EndedAt.After(time.Now()) {
		t.Errorf("ended at = %s, want between %s and now", run.EndedAt, before)
	}
}

// A supervisor that is alive holds its ticket, and the reconcile of a command
// that runs beside it must leave the run alone.
func TestReconcileLeavesATicketWhoseRunIsAlive(t *testing.T) {
	s, id := oneTicket(t)
	if _, err := s.Claim(id, "delegator/1-my-ticket"); err != nil {
		t.Fatal(err)
	}

	marked, err := s.Reconcile(allRunning)
	if err != nil {
		t.Fatal(err)
	}
	if marked != 0 {
		t.Errorf("Reconcile marked %d tickets, want none", marked)
	}

	ticket, err := s.Ticket(id)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Status != Running {
		t.Errorf("status = %q, want %q", ticket.Status, Running)
	}
	run, err := s.Run(id)
	if err != nil {
		t.Fatal(err)
	}
	if !run.EndedAt.IsZero() {
		t.Errorf("ended at = %s, want no end: the run is going", run.EndedAt)
	}
}

// The question is asked about the run, so Reconcile gives the whole row: the
// process id to signal and the start time to compare with the boot time.
func TestReconcileAsksAboutTheRunOfEachTicketInRunning(t *testing.T) {
	s, ids := threeTickets(t)
	if _, err := s.Claim(ids[0], "delegator/1-first"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(ids[1], "delegator/2-second"); err != nil {
		t.Fatal(err)
	}
	// The second ticket ends in ready, so its run is over and no question
	// belongs to it. The third was never claimed.
	if err := s.FinishTicket(ids[1], "abc123"); err != nil {
		t.Fatal(err)
	}

	var asked []Run
	if _, err := s.Reconcile(askedRuns(&asked)); err != nil {
		t.Fatal(err)
	}

	if len(asked) != 1 {
		t.Fatalf("Reconcile asked about %+v, want the run of ticket %d alone", asked, ids[0])
	}
	if asked[0].TicketID != ids[0] {
		t.Errorf("Reconcile asked about ticket %d, want %d", asked[0].TicketID, ids[0])
	}
	if asked[0].PID != os.Getpid() {
		t.Errorf("pid = %d, want %d", asked[0].PID, os.Getpid())
	}
	if asked[0].StartedAt.IsZero() {
		t.Error("the run was given with no start time")
	}
}

// A ticket that failed and restarted has one row for each claim, and
// only the last one can still be alive.
func TestReconcileAsksAboutTheLastRunOfATicket(t *testing.T) {
	s, id := oneTicket(t)
	if _, err := s.Claim(id, "delegator/1-my-ticket"); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeStatus(id, Failed); err != nil {
		t.Fatal(err)
	}
	last, err := s.Restart(id)
	if err != nil {
		t.Fatal(err)
	}

	var asked []Run
	if _, err := s.Reconcile(askedRuns(&asked)); err != nil {
		t.Fatal(err)
	}

	if len(asked) != 1 || asked[0].ID != last {
		t.Errorf("Reconcile asked about %+v, want run %d alone", asked, last)
	}
}

// Each dead run is marked in the one transaction, so no command sees the queue
// part way through a reconcile.
func TestReconcileMarksEachDeadRun(t *testing.T) {
	s, ids := threeTickets(t)
	for _, id := range ids[:2] {
		if _, err := s.Claim(id, "delegator/branch"); err != nil {
			t.Fatal(err)
		}
	}

	marked, err := s.Reconcile(noneRunning)
	if err != nil {
		t.Fatal(err)
	}
	if marked != 2 {
		t.Errorf("Reconcile marked %d tickets, want 2", marked)
	}
	for _, id := range ids[:2] {
		ticket, err := s.Ticket(id)
		if err != nil {
			t.Fatal(err)
		}
		if ticket.Status != Failed {
			t.Errorf("ticket %d is %q, want %q", id, ticket.Status, Failed)
		}
	}
}

func TestFailUnfinishedOnARunThatIsNotThere(t *testing.T) {
	s, _ := oneTicket(t)

	if err := s.FailUnfinished(404); !errors.Is(err, ErrNoRun) {
		t.Errorf("err = %v, want ErrNoRun", err)
	}
}

// A cancel closes a ticket from each state that is not the end, whether or not
// it ever had a run. A ticket with no run to close is cancelled with the run id
// 0, which no run has.
func TestCancelClosesATicket(t *testing.T) {
	for _, status := range []TicketStatus{Queued, Running, Ready, Failed} {
		s, id := oneTicket(t)
		// A new ticket is queued already, and setStatus clears the position,
		// which the CHECK of the table does not allow for that state.
		if status != Queued {
			setStatus(t, s, id, status)
		}

		if err := s.Cancel(id, 0); err != nil {
			t.Fatalf("%s: %v", status, err)
		}

		ticket, err := s.Ticket(id)
		if err != nil {
			t.Fatal(err)
		}
		if ticket.Status != Cancelled {
			t.Errorf("%s gave the status %q, want %q", status, ticket.Status, Cancelled)
		}
	}
}

// The run of a ticket that a person cancels stops with the ticket, so the row
// of that run is closed and no run of a ticket that is not running is open.
func TestCancelWritesTheEndOfTheRun(t *testing.T) {
	s, id := oneTicket(t)
	runID, err := s.Claim(id, "delegator/1-my-ticket")
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now().UTC().Truncate(time.Second)

	if err := s.Cancel(id, runID); err != nil {
		t.Fatal(err)
	}

	run, err := s.Run(id)
	if err != nil {
		t.Fatal(err)
	}
	if run.EndedAt.Before(before) || run.EndedAt.After(time.Now()) {
		t.Errorf("ended at = %s, want between %s and now", run.EndedAt, before)
	}
}

// The end time of a run that has one is the moment that run ended, and the
// cancel comes after it: the reconcile wrote it, or the supervisor did before
// it stopped.
func TestCancelKeepsTheEndThatTheRunHas(t *testing.T) {
	s, id := oneTicket(t)
	runID, err := s.Claim(id, "delegator/1-my-ticket")
	if err != nil {
		t.Fatal(err)
	}
	const ended = "2026-08-28T09:00:00Z"
	setEndedAt(t, s, id, ended)
	setStatus(t, s, id, Failed)

	if err := s.Cancel(id, runID); err != nil {
		t.Fatal(err)
	}

	rows := runRows(t, s, id)
	if rows[0].endedAt.String != ended {
		t.Errorf("ended_at = %+v, want %q", rows[0].endedAt, ended)
	}
}

// The run that a cancel closes is the run the caller named, and not whichever
// run of the ticket is the last one. A ticket that failed between the stop and
// the write restarted, and a new supervisor holds it. An end time on its row
// would say that a run which is going has ended.
func TestCancelWritesTheEndOfTheRunItIsGiven(t *testing.T) {
	s, id := oneTicket(t)
	stopped, err := s.Claim(id, "delegator/1-my-ticket")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FailUnfinished(stopped); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Restart(id); err != nil {
		t.Fatal(err)
	}
	// FailUnfinished closed the run the cancel names, so this reopens it: the
	// cancel has to reach that row and not the row of the claim after it.
	if _, err := s.db.Exec("UPDATE runs SET ended_at = NULL WHERE id = ?", stopped); err != nil {
		t.Fatal(err)
	}

	if err := s.Cancel(id, stopped); err != nil {
		t.Fatal(err)
	}

	rows := runRows(t, s, id)
	if !rows[0].endedAt.Valid {
		t.Error("the run the cancel named has no end")
	}
	if rows[1].endedAt.Valid {
		t.Errorf("the run of the later claim ended at %+v, want no end", rows[1].endedAt)
	}
}

// done and cancelled are the end, and a cancel that reached one would reopen a
// ticket that is closed. The run of the ticket is left as it is with the state.
func TestCancelRefusesATicketThatIsClosed(t *testing.T) {
	for _, status := range []TicketStatus{Done, Cancelled} {
		s, id := oneTicket(t)
		runID, err := s.Claim(id, "delegator/1-my-ticket")
		if err != nil {
			t.Fatal(err)
		}
		setStatus(t, s, id, status)

		err = s.Cancel(id, runID)
		if !errors.Is(err, ErrInvalidTicketStateChange) {
			t.Fatalf("%s: err = %v, want ErrInvalidTicketStateChange", status, err)
		}

		ticket, err := s.Ticket(id)
		if err != nil {
			t.Fatal(err)
		}
		if ticket.Status != status {
			t.Errorf("status = %q, want %q", ticket.Status, status)
		}
		if rows := runRows(t, s, id); rows[0].endedAt.Valid {
			t.Errorf("the run ended at %+v, want no end", rows[0].endedAt)
		}
	}
}

// A cancel on an id that no ticket has writes nothing and says so.
func TestCancelWithNoSuchTicket(t *testing.T) {
	s, _ := oneTicket(t)
	if err := s.Cancel(404, 0); !errors.Is(err, ErrNoTicket) {
		t.Errorf("err = %v, want ErrNoTicket", err)
	}
}

// A finish on a running ticket records the commit, puts the ticket at the end
// of READY and writes the change into its history.
func TestFinishTicketOnARunningTicket(t *testing.T) {
	s, _ := threeReady(t)
	fourth, err := s.AddTicket(mustProject(t, s), "fourth")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(fourth, "delegator/4-fourth"); err != nil {
		t.Fatal(err)
	}

	if err := s.FinishTicket(fourth, "abc1234"); err != nil {
		t.Fatal(err)
	}

	ticket, err := s.Ticket(fourth)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Status != Ready {
		t.Errorf("status = %s, want ready", ticket.Status)
	}
	if ticket.Commit != "abc1234" {
		t.Errorf("commit_id = %q, want abc1234", ticket.Commit)
	}
	wantReady := []string{"first", "second", "third", "fourth"}
	if got := readyTitles(t, s); !slices.Equal(got, wantReady) {
		t.Errorf("READY is %v, want %v", got, wantReady)
	}
	wantSteps := []string{"new to queued", "queued to running", "running to ready"}
	if got := steps(t, s, fourth); !slices.Equal(got, wantSteps) {
		t.Errorf("the history is\n%v\nwant\n%v", got, wantSteps)
	}
}

// A commit that was amended or rebased after the finish has a new hash, and a
// second finish writes it. The ticket is ready already, so nothing else about
// it moves: it keeps its place in READY, which decides the turn dg accept
// takes, and its history, which the age dg show prints comes from.
func TestFinishTicketOnAReadyTicketReplacesTheCommit(t *testing.T) {
	s, ids := threeReady(t)
	if err := s.FinishTicket(ids[1], "abc1234"); err != nil {
		t.Fatal(err)
	}
	position := ticketPosition(t, s, "ready_position", ids[1])
	history := steps(t, s, ids[1])

	if err := s.FinishTicket(ids[1], "def5678"); err != nil {
		t.Fatal(err)
	}

	ticket, err := s.Ticket(ids[1])
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Commit != "def5678" {
		t.Errorf("commit_id = %q, want def5678", ticket.Commit)
	}
	if ticket.Status != Ready {
		t.Errorf("status = %s, want ready", ticket.Status)
	}
	if got := ticketPosition(t, s, "ready_position", ids[1]); got != position {
		t.Errorf("ready_position = %+v, want %+v", got, position)
	}
	wantReady := []string{"first", "second", "third"}
	if got := readyTitles(t, s); !slices.Equal(got, wantReady) {
		t.Errorf("READY is %v, want %v", got, wantReady)
	}
	if got := steps(t, s, ids[1]); !slices.Equal(got, history) {
		t.Errorf("the history is\n%v\nwant\n%v", got, history)
	}
}

// A ticket that is closed is past the point of taking a commit, and a finish on
// one writes nothing and names the change it refused.
func TestFinishTicketOnAClosedTicket(t *testing.T) {
	for _, status := range []TicketStatus{Done, Cancelled} {
		t.Run(string(status), func(t *testing.T) {
			s, ids := threeReady(t)
			if err := s.ChangeStatus(ids[1], status); err != nil {
				t.Fatal(err)
			}

			err := s.FinishTicket(ids[1], "abc1234")
			if !errors.Is(err, ErrInvalidTicketStateChange) {
				t.Fatalf("err = %v, want ErrInvalidTicketStateChange", err)
			}

			ticket, err := s.Ticket(ids[1])
			if err != nil {
				t.Fatal(err)
			}
			if ticket.Commit != "" {
				t.Errorf("commit_id = %q, want none", ticket.Commit)
			}
			if ticket.Status != status {
				t.Errorf("status = %s, want %s", ticket.Status, status)
			}
		})
	}
}

// dg list shows every ticket, so AllTickets holds each status and not the four
// that are open. A done ticket that the window of the inbox has passed is the
// one a person goes to this list for: the inbox stops showing it after a day,
// and the work it names does not stop existing then.
func TestAllTicketsGivesEveryTicketWhateverItsStatus(t *testing.T) {
	s, projectID := emptyStore(t)
	want := map[int64]TicketStatus{}
	var done int64
	for _, status := range []TicketStatus{Queued, Running, Ready, Failed, Done, Cancelled} {
		id, err := s.AddTicket(projectID, string(status))
		if err != nil {
			t.Fatal(err)
		}
		// A new ticket is queued and holds a position, which setStatus takes
		// away.
		if status != Queued {
			setStatus(t, s, id, status)
		}
		want[id] = status
		if status == Done {
			done = id
		}
	}
	// The person accepted the done ticket two days ago, which is outside the
	// window that DoneTickets reads.
	setAccepted(t, s, done, rfc3339(time.Now().Add(-48*time.Hour)))

	all, err := s.AllTickets("")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != len(want) {
		t.Fatalf("AllTickets gives %d tickets, want %d", len(all), len(want))
	}
	for _, ticket := range all {
		if status := want[ticket.ID]; ticket.Status != status {
			t.Errorf("ticket %d has the status %q, want %q", ticket.ID, ticket.Status, status)
		}
	}
}

// The list is in the order of the ids, which is the order the tickets were
// made in. A person who reads it goes to a ticket by its number.
func TestAllTicketsComesInTheOrderOfTheIDs(t *testing.T) {
	s, ids := threeTickets(t)
	if err := s.MoveTicket(ids[2], Top); err != nil {
		t.Fatal(err)
	}

	all, err := s.AllTickets("")
	if err != nil {
		t.Fatal(err)
	}
	var got []int64
	for _, ticket := range all {
		got = append(got, ticket.ID)
	}
	if !slices.Equal(got, ids) {
		t.Errorf("AllTickets gives the ids %v, want %v", got, ids)
	}
}

// A path takes the list to the tickets of one project, which is dg list
// --project. The query does the work, so a list of one project of many reads
// only the rows it gives back.
func TestAllTicketsTakesTheTicketsOfOneProject(t *testing.T) {
	s, _, second := twoProjects(t)

	all, err := s.AllTickets("/projects/other")
	if err != nil {
		t.Fatal(err)
	}
	var got []int64
	for _, ticket := range all {
		got = append(got, ticket.ID)
		if ticket.Project != "/projects/other" {
			t.Errorf("ticket %d has the project %q, want /projects/other", ticket.ID, ticket.Project)
		}
	}
	if !slices.Equal(got, second) {
		t.Errorf("AllTickets gives the ids %v, want the ids %v of that project", got, second)
	}
}

// A path that no project holds gives no ticket, and it is not an error: the
// person named a directory that delegator has no ticket for.
func TestAllTicketsOfAProjectThatHasNone(t *testing.T) {
	s, _ := threeTickets(t)

	all, err := s.AllTickets("/projects/nothing")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Errorf("AllTickets gives %d tickets for a path that no project holds, want none", len(all))
	}
}
