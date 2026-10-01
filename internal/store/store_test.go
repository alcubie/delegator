// Tests of Open call it directly so helpers do not hide expected migration,
// version, or permission errors.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/alcubie/delegator/internal/config"
)

const testAgentID int64 = 1

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

// queuedTickets creates tickets in title order and returns their IDs.
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

// twoPrograms opens separate connection pools on one database to simulate
// concurrent commands, with six queued tickets.
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

// claimBranch names the ticket selected inside ClaimNext's transaction.
func claimBranch(t Ticket) string {
	return fmt.Sprintf("delegator/%d-%s", t.ID, t.Title)
}

// claimableCount reads the number of eligible tickets and fails on error.
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

// readyTitles sorts ready tickets by ready_position, matching internal/inbox.
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

// ticketPosition reads position or ready_position as a nullable value.
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

// askedRuns records reconciliation probes and reports every run alive.
func askedRuns(asked *[]Run) func(Run) bool {
	return func(r Run) bool {
		*asked = append(*asked, r)
		return true
	}
}

// setEndedAt gives the latest run a distinct end timestamp so tests can
// detect overwrites despite second precision.
func setEndedAt(t *testing.T, s *Store, ticketID int64, ended string) {
	t.Helper()
	if _, err := s.db.Exec(`
		UPDATE runs SET ended_at = ?
		WHERE id = (SELECT id FROM runs WHERE ticket_id = ? ORDER BY id DESC LIMIT 1)`,
		ended, ticketID); err != nil {
		t.Fatal(err)
	}
}

// setMigrations replaces the migration list until cleanup. These tests must
// run serially.
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

	// Keep the filename literal to detect changes that would hide
	// existing databases.
	if _, err := os.Stat(filepath.Join(dataDir, "delegator.db")); err != nil {
		t.Error(err)
	}

	if got := userVersion(t, s.db); got != len(migrations) {
		t.Errorf("user_version = %d, want %d", got, len(migrations))
	}

	for _, name := range []string{"projects", "tickets", "queue_state", "agents", "models", "runs", "ticket_deps", "transitions", "settings", "run_usage"} {
		var got string
		err := s.db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", name).Scan(&got)
		if err != nil {
			t.Errorf("table %s: %v", name, err)
		}
	}
}

// Run metadata belongs to separate rows because a ticket can run more than
// once.
func TestOpenMakesTheTableRunsWithItsColumns(t *testing.T) {
	s, _ := emptyStore(t)

	want := []string{"id", "ticket_id", "pid", "started_at", "ended_at", "exit_code", "agent_id", "model_id"}
	if got := columnsOf(t, s.db, "runs"); !slices.Equal(got, want) {
		t.Errorf("the columns of runs = %v, want %v", got, want)
	}
}

func TestOpenMakesTheRunUsageTableWithNullableTokenCategories(t *testing.T) {
	s, _ := emptyStore(t)
	want := []string{
		"run_id", "input_tokens", "cached_write_tokens", "cached_read_tokens",
		"output_tokens", "thought_tokens", "total_tokens",
	}
	if got := columnsOf(t, s.db, "run_usage"); !slices.Equal(got, want) {
		t.Errorf("the columns of run_usage = %v, want %v", got, want)
	}

	rows, err := s.db.Query("PRAGMA table_info(run_usage)")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primary int
		var name, kind string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primary); err != nil {
			t.Fatal(err)
		}
		if name != "run_id" && notNull != 0 {
			t.Errorf("%s is NOT NULL, want an omitted category to remain NULL", name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
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
	cursor, err := s.Agent("cursor")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"agent", "acp"}; !slices.Equal(cursor.Argv, want) {
		t.Errorf("cursor argv = %v, want %v", cursor.Argv, want)
	}
	if len(cursor.Resume) != 0 {
		t.Errorf("cursor resume argv = %v, want none", cursor.Resume)
	}
	if cursor.InstallHint == "" {
		t.Error("cursor has no installation hint")
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
	pi, err := s.Agent("pi")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"pi-acp"}; !slices.Equal(pi.Argv, want) {
		t.Errorf("pi argv = %v, want %v", pi.Argv, want)
	}
	if want := []string{"pi", "--session", "{session}"}; !slices.Equal(pi.Resume, want) {
		t.Errorf("pi resume argv = %v, want %v", pi.Resume, want)
	}
	if pi.InstallHint == "" {
		t.Error("pi has no installation hint")
	}
	for _, name := range []string{"claude", "codex", "gemini"} {
		agent, err := s.Agent(name)
		if err != nil {
			t.Fatal(err)
		}
		if agent.InstallHint == "" {
			t.Errorf("%s has no installation hint", name)
		}
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

func TestAgentByIDReturnsTheRegistryEntry(t *testing.T) {
	s, _ := emptyStore(t)
	want, err := s.Agent("codex")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.AgentByID(want.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("agent = %+v, want %+v", got, want)
	}
}

func TestOpenMakesTheDirectories(t *testing.T) {
	// Use a missing child directory to check creation of the data root
	// itself.
	dataDir := filepath.Join(t.TempDir(), "delegator")

	s, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// The empty name checks the data root. Literal permissions ensure
	// changing the production constant cannot silently change
	// expectations.
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

func TestOpenWithAStepThatFailsChangesNothing(t *testing.T) {
	dataDir := t.TempDir()
	first, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	first.Close()

	// Fail after creating a table to verify the entire migration rolls
	// back.
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

// An older binary must refuse a newer schema without changing it.
func TestOpenWithADatabaseFromALaterVersion(t *testing.T) {
	dataDir := t.TempDir()
	first, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	first.Close()

	// Simulate a schema version from a newer binary.
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

// Hold the first connection while opening a second to verify busy_timeout
// applies to every pooled connection.
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

// URL delimiters in paths must be escaped so SQLite opens the intended file.
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

	// Reverse ID order to distinguish position sorting. Temporary
	// negative positions avoid unique-index collisions without violating
	// the non-NULL constraint.
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

// Opening an older database must apply each missing migration exactly once.
func TestOpenAppliesANewStepToAnOldDatabase(t *testing.T) {
	dataDir := t.TempDir()

	first, err := Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	projectID, err := first.AddProject("/projects/path", "main")
	if err != nil {
		t.Fatal(err)
	}
	first.Close()

	setMigrations(t, append(migrations,
		"CREATE TABLE later (id INTEGER PRIMARY KEY);"))

	if got := userVersion(t, openRaw(t, dataDir)); got != len(migrations)-1 {
		t.Fatalf("the old database is at version %d, want %d", got, len(migrations)-1)
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
		"SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'later'").Scan(&name)
	if err != nil {
		t.Errorf("the table of the new step is not on the old database: %v", err)
	}
	var id int64
	if err := second.db.QueryRow("SELECT id FROM projects WHERE id = ?", projectID).Scan(&id); err != nil {
		t.Errorf("the existing project did not survive migration: %v", err)
	}
	second.Close()
	reopened, err := Open(dataDir)
	if err != nil {
		t.Fatalf("reopening an up-to-date database: %v", err)
	}
	reopened.Close()
}

func TestAddTicketPutsTheTicketAtTheEndOfTheQueue(t *testing.T) {
	s, ids := threeTickets(t)

	want := []string{"first", "second", "third"}
	if got := queueTitles(t, s); !slices.Equal(got, want) {
		t.Errorf("the queue is %v, want %v", got, want)
	}

	// Normalize positions to a contiguous sequence starting at 1.
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

// Enforce queued status and non-NULL position together; neither half is valid
// alone.
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
	// Collect concurrent errors through a buffered channel to avoid a
	// shared-slice race or blocked sends before joining workers.
	errs := make(chan error, moves)
	for i := range moves {
		program := first
		if i%2 == 1 {
			program = second
		}
		// Register workers before launching them so Wait cannot
		// return early.
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

	// Check contiguous positions to catch lost concurrent updates.
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
	// The initial project registration must retain its default branch.
	if projects[0].DefaultBranch != "main" {
		t.Errorf("default branch = %q, want main", projects[0].DefaultBranch)
	}
}

// Database permissions must protect ticket data regardless of the user's
// umask.
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

// setStatus bypasses transition rules to arrange non-queued test states.
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

// One inbox query must return tickets across projects.
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
	// Check row count as well as paths to catch an accidental cross join.
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

// setAccepted inserts an acceptance transition without running the full
// command workflow.
func setAccepted(t *testing.T, s *Store, id int64, accepted string) {
	t.Helper()
	if _, err := s.db.Exec(`
		INSERT INTO transitions (ticket_id, from_status, to_status, at)
		VALUES (?, 'ready', 'done', ?)`, id, accepted); err != nil {
		t.Fatal(err)
	}
}

// setReady inserts a ready transition to distinguish completion from
// acceptance.
func setReady(t *testing.T, s *Store, id int64, ready string) {
	t.Helper()
	if _, err := s.db.Exec(`
		INSERT INTO transitions (ticket_id, from_status, to_status, at)
		VALUES (?, 'running', 'ready', ?)`, id, ready); err != nil {
		t.Fatal(err)
	}
}

// A ready timestamp is not an acceptance timestamp.
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

// Creation time must come from the initial history entry.
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

// setStarted assigns distinct run timestamps; normal claims in one test may
// share a second.
func setStarted(t *testing.T, s *Store, runID int64, started string) {
	t.Helper()
	if _, err := s.db.Exec(
		"UPDATE runs SET started_at = ? WHERE id = ?", started, runID); err != nil {
		t.Fatal(err)
	}
}

// Expose the run start for elapsed time, or zero for never-run tickets.
func TestOpenTicketsGivesTheStartOfTheRun(t *testing.T) {
	s, ids := threeTickets(t)
	if _, err := s.Claim(ids[0], "delegator/1-first", testAgentID); err != nil {
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

// Use the latest run start after a restart, not the earlier failed run's
// time.
func TestOpenTicketsGivesTheStartOfTheLastRun(t *testing.T) {
	s, id := oneTicket(t)
	if _, err := s.Claim(id, "delegator/1-my-ticket", testAgentID); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeStatus(id, Failed); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Restart(id, testAgentID, sql.Null[int64]{}); err != nil {
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

// setFailed inserts a failure transition to check which timestamp the inbox
// reads.
func setFailed(t *testing.T, s *Store, id int64, failed string) {
	t.Helper()
	if _, err := s.db.Exec(`
		INSERT INTO transitions (ticket_id, from_status, to_status, at)
		VALUES (?, 'running', 'failed', ?)`, id, failed); err != nil {
		t.Fatal(err)
	}
}

// Failed tickets remain visible with their failure timestamp for ordering.
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

// Reordering READY must leave the queue unchanged.
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

// Moves cannot cross from READY into QUEUED.
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

func TestChangeStatusFromAnEnd(t *testing.T) {
	s, id := oneTicket(t)
	if err := s.ChangeStatus(id, Cancelled); err != nil {
		t.Fatal(err)
	}

	if err := s.ChangeStatus(id, Running); !errors.Is(err, ErrInvalidTicketStateChange) {
		t.Errorf("err = %v, want ErrInvalidTicketStateChange", err)
	}
}

// New ready tickets append after existing review work.
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

// Leaving READY must clear ready_position; unlike the queue column, no CHECK
// enforces this.
func TestChangeStatusOutOfReadyClearsTheReadyPosition(t *testing.T) {
	for _, status := range []TicketStatus{Done, Cancelled} {
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

func TestChangeStatusFromReadyToQueuedIsRefused(t *testing.T) {
	s, ids := threeTickets(t)

	for _, status := range []TicketStatus{Running, Ready} {
		if err := s.ChangeStatus(ids[0], status); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ChangeStatus(ids[0], Queued); !errors.Is(err, ErrInvalidTicketStateChange) {
		t.Fatalf("err = %v, want ErrInvalidTicketStateChange", err)
	}

	want := []string{"second", "third"}
	if got := queueTitles(t, s); !slices.Equal(got, want) {
		t.Errorf("the queue is %v, want %v", got, want)
	}
	if got := readyTitles(t, s); !slices.Equal(got, []string{"first"}) {
		t.Errorf("READY is %v, want [first]", got)
	}
}

func TestChangeStatusOnATicketThatIsNotThere(t *testing.T) {
	s, _ := emptyStore(t)

	if err := s.ChangeStatus(9999, Running); !errors.Is(err, ErrNoTicket) {
		t.Errorf("err = %v, want ErrNoTicket", err)
	}
}

// Claim records the caller's supervisor PID and a UTC start timestamp.
func TestClaimWritesARunWithThePidAndTheStartTime(t *testing.T) {
	s, id := oneTicket(t)
	before := time.Now().UTC().Truncate(time.Second)

	if _, err := s.Claim(id, "delegator/1-my-ticket", testAgentID); err != nil {
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

// A losing claim must roll back without creating a run.
func TestClaimThatIsRefusedWritesNoRun(t *testing.T) {
	s, id := oneTicket(t)
	if _, err := s.Claim(id, "delegator/1-my-ticket", testAgentID); err != nil {
		t.Fatal(err)
	}

	runID, err := s.Claim(id, "delegator/1-my-ticket", testAgentID)
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

// Selection and claim share a transaction.
func TestClaimNextTakesTheFirstTicketOfTheQueue(t *testing.T) {
	s, ids := threeTickets(t)
	// User-set position, not ID, determines which ticket runs first.
	if err := s.MoveTicket(ids[2], Top); err != nil {
		t.Fatal(err)
	}

	claimed, runID, err := s.ClaimNext(config.Config{Runs: 1}, claimBranch, testAgentID)
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

func TestClaimNextWithAnEmptyQueueClaimsNothing(t *testing.T) {
	s, _ := emptyStore(t)

	claimed, runID, err := s.ClaimNext(config.Config{Runs: 1}, claimBranch, testAgentID)

	if !errors.Is(err, ErrNoRoom) {
		t.Fatalf("err = %v, want ErrNoRoom", err)
	}
	if claimed.ID != 0 || runID != 0 {
		t.Errorf("claimed ticket %d as run %d, want nothing", claimed.ID, runID)
	}
}

// Running and ready tickets each consume a slot at limit one.
func TestClaimNextWithATicketThatHoldsTheQueue(t *testing.T) {
	for _, status := range []TicketStatus{Running, Ready} {
		s, ids := threeTickets(t)
		if _, err := s.Claim(ids[0], "delegator/1-first", testAgentID); err != nil {
			t.Fatal(err)
		}
		if status == Ready {
			if err := s.ChangeStatus(ids[0], Ready); err != nil {
				t.Fatal(err)
			}
		}

		claimed, _, err := s.ClaimNext(config.Config{Runs: 1}, claimBranch, testAgentID)

		if !errors.Is(err, ErrNoRoom) {
			t.Errorf("with a ticket in %s: err = %v, want ErrNoRoom", status, err)
		}
		if claimed.ID != 0 {
			t.Errorf("with a ticket in %s: claimed ticket %d, want nothing", status, claimed.ID)
		}
	}
}

func TestClaimNextWithASlotFreeClaimsTheNextTicket(t *testing.T) {
	s, ids := threeTickets(t)
	if _, err := s.Claim(ids[0], "delegator/1-first", testAgentID); err != nil {
		t.Fatal(err)
	}

	claimed, _, err := s.ClaimNext(config.Config{Runs: 2}, claimBranch, testAgentID)

	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != ids[1] {
		t.Errorf("claimed ticket %d, want the first ticket still queued %d", claimed.ID, ids[1])
	}
}

// Ready tickets consume global capacity until reviewed.
func TestClaimNextWithEverySlotFullClaimsNothing(t *testing.T) {
	s, ids := threeTickets(t)
	for _, id := range ids[:2] {
		if _, err := s.Claim(id, claimBranch(Ticket{ID: id}), testAgentID); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ChangeStatus(ids[0], Ready); err != nil {
		t.Fatal(err)
	}

	claimed, _, err := s.ClaimNext(config.Config{Runs: 2}, claimBranch, testAgentID)

	if !errors.Is(err, ErrNoRoom) {
		t.Fatalf("err = %v, want ErrNoRoom", err)
	}
	if claimed.ID != 0 {
		t.Errorf("claimed ticket %d with both slots full, want nothing", claimed.ID)
	}
}

// Lowering capacity below current occupancy must clamp free slots to zero.
func TestClaimNextWithMoreTicketsOpenThanTheLimitClaimsNothing(t *testing.T) {
	s, ids := threeTickets(t)
	for _, id := range ids[:2] {
		if _, err := s.Claim(id, claimBranch(Ticket{ID: id}), testAgentID); err != nil {
			t.Fatal(err)
		}
	}

	claimed, _, err := s.ClaimNext(config.Config{Runs: 1}, claimBranch, testAgentID)

	if !errors.Is(err, ErrNoRoom) {
		t.Fatalf("err = %v, want ErrNoRoom", err)
	}
	if claimed.ID != 0 {
		t.Errorf("claimed ticket %d with the limit below the tickets open, want nothing", claimed.ID)
	}
}

// Automatic claims respect pause; explicit ticket claims are separate.
func TestClaimNextWithAPausedQueueClaimsNothing(t *testing.T) {
	s, _ := threeTickets(t)
	if err := s.PauseQueue(); err != nil {
		t.Fatal(err)
	}

	claimed, _, err := s.ClaimNext(config.Config{Runs: 1}, claimBranch, testAgentID)

	if !errors.Is(err, ErrNoRoom) {
		t.Fatalf("err = %v, want ErrNoRoom", err)
	}
	if claimed.ID != 0 {
		t.Errorf("claimed ticket %d on a paused queue, want nothing", claimed.ID)
	}
}

// Skip a full project to start eligible work in another project.
func TestClaimNextPassesOverAProjectAtItsLimit(t *testing.T) {
	s, first, second := twoProjects(t)
	if _, err := s.Claim(first[0], claimBranch(Ticket{ID: first[0]}), testAgentID); err != nil {
		t.Fatal(err)
	}

	claimed, _, err := s.ClaimNext(config.Config{Runs: 3, MaxRunsPerProject: 1}, claimBranch, testAgentID)

	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != second[0] {
		t.Errorf("claimed ticket %d, want the first ticket of the other project %d",
			claimed.ID, second[0])
	}
}

func TestClaimNextTakesASecondTicketOfAProjectWithRoom(t *testing.T) {
	s, first, _ := twoProjects(t)
	if _, err := s.Claim(first[0], claimBranch(Ticket{ID: first[0]}), testAgentID); err != nil {
		t.Fatal(err)
	}

	claimed, _, err := s.ClaimNext(config.Config{Runs: 3, MaxRunsPerProject: 2}, claimBranch, testAgentID)

	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != first[1] {
		t.Errorf("claimed ticket %d, want the second ticket of the same project %d",
			claimed.ID, first[1])
	}
}

// Global capacity alone is insufficient when every queued project is full.
func TestClaimNextWithEveryProjectAtItsLimitClaimsNothing(t *testing.T) {
	s, first, second := twoProjects(t)
	for _, id := range []int64{first[0], second[0]} {
		if _, err := s.Claim(id, claimBranch(Ticket{ID: id}), testAgentID); err != nil {
			t.Fatal(err)
		}
	}

	claimed, _, err := s.ClaimNext(config.Config{Runs: 4, MaxRunsPerProject: 1}, claimBranch, testAgentID)

	if !errors.Is(err, ErrNoRoom) {
		t.Fatalf("err = %v, want ErrNoRoom", err)
	}
	if claimed.ID != 0 {
		t.Errorf("claimed ticket %d with every project at its limit, want nothing", claimed.ID)
	}
}

// Ready tickets also consume per-project capacity.
func TestClaimNextCountsAReadyTicketAgainstItsProject(t *testing.T) {
	s, first, second := twoProjects(t)
	if _, err := s.Claim(first[0], claimBranch(Ticket{ID: first[0]}), testAgentID); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeStatus(first[0], Ready); err != nil {
		t.Fatal(err)
	}

	claimed, _, err := s.ClaimNext(config.Config{Runs: 3, MaxRunsPerProject: 1}, claimBranch, testAgentID)

	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != second[0] {
		t.Errorf("claimed ticket %d, want the first ticket of the other project %d",
			claimed.ID, second[0])
	}
}

func TestClaimableCountCountsTheTicketsOfTheQueue(t *testing.T) {
	s, _ := threeTickets(t)

	if got := claimableCount(t, s, config.Config{Runs: 4}); got != 3 {
		t.Errorf("count = %d, want 3: every ticket of the queue can be claimed", got)
	}
}

// Global capacity caps the sum of per-project claimable counts.
func TestClaimableCountStopsAtTheFreeSlots(t *testing.T) {
	s, first, _ := twoProjects(t)
	if _, err := s.Claim(first[0], claimBranch(Ticket{ID: first[0]}), testAgentID); err != nil {
		t.Fatal(err)
	}

	got := claimableCount(t, s, config.Config{Runs: 2, MaxRunsPerProject: 2})

	if got != 1 {
		t.Errorf("count = %d with one slot of the queue free, want 1", got)
	}
}

// Do not launch supervisors into an otherwise empty queue whose projects are
// all full.
func TestClaimableCountWithEveryProjectAtItsLimitCountsNothing(t *testing.T) {
	s, first, second := twoProjects(t)
	for _, id := range []int64{first[0], second[0]} {
		if _, err := s.Claim(id, claimBranch(Ticket{ID: id}), testAgentID); err != nil {
			t.Fatal(err)
		}
	}

	got := claimableCount(t, s, config.Config{Runs: 4, MaxRunsPerProject: 1})

	if got != 0 {
		t.Errorf("count = %d with two slots free and every project at its limit, want 0", got)
	}
}

// Two currently eligible tickets in one project still count as one if only
// one project slot remains.
func TestClaimableCountStopsAtTheRoomOfOneProject(t *testing.T) {
	s, _ := threeTickets(t)

	got := claimableCount(t, s, config.Config{Runs: 4, MaxRunsPerProject: 1})

	if got != 1 {
		t.Errorf("count = %d for three tickets of one project with a limit of one, want 1", got)
	}
}

// Apply the project limit separately to each project.
func TestClaimableCountCountsTheRoomOfEveryProject(t *testing.T) {
	s, _, _ := twoProjects(t)

	got := claimableCount(t, s, config.Config{Runs: 4, MaxRunsPerProject: 1})

	if got != 2 {
		t.Errorf("count = %d for two projects with a place each, want 2", got)
	}
}

// Concurrent claims of the last ticket must produce one winner.
func TestTwoSupervisorsThatClaimNextTakeOneTicket(t *testing.T) {
	first, second, ids := twoPrograms(t)

	var wg sync.WaitGroup
	claims := make(chan Ticket, 2)
	errs := make(chan error, 2)
	for _, s := range []*Store{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claimed, _, err := s.ClaimNext(config.Config{Runs: 1}, claimBranch, testAgentID)
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

// With two slots, concurrent supervisors must claim distinct tickets.
func TestTwoSupervisorsWithTwoSlotsTakeTwoTickets(t *testing.T) {
	first, second, ids := twoPrograms(t)

	var wg sync.WaitGroup
	claims := make(chan Ticket, 2)
	for _, s := range []*Store{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claimed, _, err := s.ClaimNext(config.Config{Runs: 2}, claimBranch, testAgentID)
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

// Liveness checks receive the supervisor PID and run start time.
func TestRunGivesThePidAndTheStartTimeOfTheRun(t *testing.T) {
	s, id := oneTicket(t)
	if _, err := s.Claim(id, "delegator/1-my-ticket", testAgentID); err != nil {
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
	agentID, err := s.AgentID("mine")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(id, "delegator/1-my-ticket", agentID); err != nil {
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
	if _, err := s.Claim(id, "delegator/1-my-ticket", 404); err == nil {
		t.Fatal("Claim accepted an id that no agent has")
	}
	ticket, err := s.Ticket(id)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Status != Queued {
		t.Errorf("status = %q, want %q after the failed claim", ticket.Status, Queued)
	}
}

func usageInt(n int) *int { return &n }

func TestRunUsageKeepsEveryReportedCategoryIncludingZero(t *testing.T) {
	s, id := oneTicket(t)
	runID, err := s.Claim(id, "delegator/1-my-ticket", testAgentID)
	if err != nil {
		t.Fatal(err)
	}
	want := AggregateUsage{
		InputTokens:       usageInt(12),
		CachedWriteTokens: usageInt(0),
		CachedReadTokens:  usageInt(3),
		OutputTokens:      usageInt(4),
		ThoughtTokens:     usageInt(2),
		TotalTokens:       usageInt(17),
	}
	if err := s.AddRunUsage(runID, want); err != nil {
		t.Fatal(err)
	}

	got, present, err := s.RunUsage(runID)
	if err != nil {
		t.Fatal(err)
	}
	if !present {
		t.Fatal("the run has no usage row")
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("usage = %+v, want %+v", got, want)
	}
}

func TestRunWithNoReportedUsageHasNoUsageRow(t *testing.T) {
	s, id := oneTicket(t)
	runID, err := s.Claim(id, "delegator/1-my-ticket", testAgentID)
	if err != nil {
		t.Fatal(err)
	}

	got, present, err := s.RunUsage(runID)
	if err != nil {
		t.Fatal(err)
	}
	if present {
		t.Errorf("usage = %+v, want no usage row", got)
	}
}

func TestRunUsageRefusesARunThatDoesNotExist(t *testing.T) {
	s, _ := oneTicket(t)
	usage := AggregateUsage{InputTokens: usageInt(1), OutputTokens: usageInt(2), TotalTokens: usageInt(3)}
	if err := s.AddRunUsage(404, usage); !errors.Is(err, ErrNoRun) {
		t.Errorf("err = %v, want ErrNoRun", err)
	}
}

// A run normally has one prompt, but aggregation does not turn an optional
// category omitted by either of two prompts into a partial count. Required
// categories and independently complete optional categories are summed.
func TestRunUsageAggregatesPromptsWithoutFillingMissingCategories(t *testing.T) {
	s, id := oneTicket(t)
	runID, err := s.Claim(id, "delegator/1-my-ticket", testAgentID)
	if err != nil {
		t.Fatal(err)
	}
	first := AggregateUsage{
		InputTokens:       usageInt(10),
		CachedWriteTokens: usageInt(2),
		CachedReadTokens:  nil,
		OutputTokens:      usageInt(5),
		ThoughtTokens:     usageInt(3),
		TotalTokens:       usageInt(17),
	}
	second := AggregateUsage{
		InputTokens:       usageInt(20),
		CachedWriteTokens: nil,
		CachedReadTokens:  usageInt(7),
		OutputTokens:      usageInt(11),
		ThoughtTokens:     usageInt(4),
		TotalTokens:       usageInt(38),
	}
	if err := s.AddRunUsage(runID, first); err != nil {
		t.Fatal(err)
	}
	if err := s.AddRunUsage(runID, second); err != nil {
		t.Fatal(err)
	}

	got, present, err := s.RunUsage(runID)
	if err != nil {
		t.Fatal(err)
	}
	want := AggregateUsage{
		InputTokens:       usageInt(30),
		CachedWriteTokens: nil,
		CachedReadTokens:  nil,
		OutputTokens:      usageInt(16),
		ThoughtTokens:     usageInt(7),
		TotalTokens:       usageInt(55),
	}
	if !present || !reflect.DeepEqual(got, want) {
		t.Errorf("usage = (%+v, %t), want (%+v, true)", got, present, want)
	}

	var rows int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM run_usage WHERE run_id = ?", runID).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("the run has %d usage rows after two prompts, want 1", rows)
	}
}

// Run returns the latest attempt, not the failed predecessor.
func TestRunGivesTheLastRunOfATicket(t *testing.T) {
	s, id := oneTicket(t)
	if _, err := s.Claim(id, "delegator/1-my-ticket", testAgentID); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeStatus(id, Failed); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Restart(id, testAgentID, sql.Null[int64]{}); err != nil {
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

// Both missing tickets and never-run tickets return ErrNoRun with the
// requested ID.
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

func TestEndRunWritesTheEndTimeAndTheExitCode(t *testing.T) {
	for _, exitCode := range []int{0, 3} {
		s, id := oneTicket(t)
		runID, err := s.Claim(id, "delegator/1-my-ticket", testAgentID)
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

// Ending an older run must not close the run created by a restart.
func TestEndRunEndsTheRunItWasGiven(t *testing.T) {
	s, id := oneTicket(t)
	first, err := s.Claim(id, "delegator/1-my-ticket", testAgentID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeStatus(id, Failed); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Restart(id, testAgentID, sql.Null[int64]{}); err != nil {
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

// The acceptance cutoff is inclusive.
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

// Normalize the cutoff to UTC before comparing stored timestamp text.
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

// Exclude cancelled and still-open tickets from DONE.
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

// Legacy done tickets without acceptance history must remain outside every
// window.
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

// Recent acceptance must include work completed before the window began.
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

// A zero-length window must exclude tickets accepted earlier in the current
// second.
func TestDoneTicketsWithAWindowOfNoLengthHoldsNothing(t *testing.T) {
	s, id := oneTicket(t)
	setStatus(t, s, id, Done)
	setAccepted(t, s, id, "2026-08-28T09:00:00Z")

	// Place the cutoff partway through the ticket's acceptance second.
	done, err := s.DoneTickets(time.Date(2026, 8, 28, 9, 0, 0, 500, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(done) != 0 {
		t.Errorf("DoneTickets gives %v, want none", ticketIDs(done))
	}
}

// Preserve an end time already recorded by the supervisor.
func TestFailUnfinishedKeepsTheEndThatTheRunHas(t *testing.T) {
	s, id := oneTicket(t)
	runID, err := s.Claim(id, "delegator/1-my-ticket", testAgentID)
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

// Fill a missing end time when the supervisor could not record it.
func TestFailUnfinishedWritesTheEndOfARunThatHasNone(t *testing.T) {
	s, id := oneTicket(t)
	runID, err := s.Claim(id, "delegator/1-my-ticket", testAgentID)
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

// FailUnfinished must preserve Ready and Cancelled statuses.
func TestFailUnfinishedLeavesATicketThatIsNotRunning(t *testing.T) {
	for _, status := range []TicketStatus{Ready, Failed, Cancelled} {
		s, id := oneTicket(t)
		runID, err := s.Claim(id, "delegator/1-my-ticket", testAgentID)
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

// Reconcile recovers abandoned running tickets and closes their runs.
func TestReconcileFailsATicketWhoseRunIsDead(t *testing.T) {
	s, id := oneTicket(t)
	if _, err := s.Claim(id, "delegator/1-my-ticket", testAgentID); err != nil {
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

func TestReconcileLeavesATicketWhoseRunIsAlive(t *testing.T) {
	s, id := oneTicket(t)
	if _, err := s.Claim(id, "delegator/1-my-ticket", testAgentID); err != nil {
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

// Pass the full run to liveness checks for PID and boot-time comparison.
func TestReconcileAsksAboutTheRunOfEachTicketInRunning(t *testing.T) {
	s, ids := threeTickets(t)
	if _, err := s.Claim(ids[0], "delegator/1-first", testAgentID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(ids[1], "delegator/2-second", testAgentID); err != nil {
		t.Fatal(err)
	}
	// Only the first ticket is still running; ready and never-claimed
	// tickets need no probe.
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

// Only the latest run is relevant to reconciliation.
func TestReconcileAsksAboutTheLastRunOfATicket(t *testing.T) {
	s, id := oneTicket(t)
	if _, err := s.Claim(id, "delegator/1-my-ticket", testAgentID); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeStatus(id, Failed); err != nil {
		t.Fatal(err)
	}
	last, err := s.Restart(id, testAgentID, sql.Null[int64]{})
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

// Reconcile all dead runs atomically.
func TestReconcileMarksEachDeadRun(t *testing.T) {
	s, ids := threeTickets(t)
	for _, id := range ids[:2] {
		if _, err := s.Claim(id, "delegator/branch", testAgentID); err != nil {
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

// Cancel accepts every non-terminal state, with run ID zero when there is no
// run to close.
func TestCancelClosesATicket(t *testing.T) {
	for _, status := range []TicketStatus{Queued, Running, Ready, Failed} {
		s, id := oneTicket(t)
		// Do not use setStatus for queued fixtures; it clears the
		// required position.
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

// Cancellation closes the named run as well as the ticket.
func TestCancelWritesTheEndOfTheRun(t *testing.T) {
	s, id := oneTicket(t)
	runID, err := s.Claim(id, "delegator/1-my-ticket", testAgentID)
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

// Cancellation must preserve an end time already written by the supervisor or
// reconciliation.
func TestCancelKeepsTheEndThatTheRunHas(t *testing.T) {
	s, id := oneTicket(t)
	runID, err := s.Claim(id, "delegator/1-my-ticket", testAgentID)
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

// Cancellation must close the named run, not a newer one created by a
// concurrent restart.
func TestCancelWritesTheEndOfTheRunItIsGiven(t *testing.T) {
	s, id := oneTicket(t)
	stopped, err := s.Claim(id, "delegator/1-my-ticket", testAgentID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FailUnfinished(stopped); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Restart(id, testAgentID, sql.Null[int64]{}); err != nil {
		t.Fatal(err)
	}
	// Reopen the old run to prove cancellation updates it rather than the
	// newer run.
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

func TestCancelRefusesATicketThatIsClosed(t *testing.T) {
	for _, status := range []TicketStatus{Done, Cancelled} {
		s, id := oneTicket(t)
		runID, err := s.Claim(id, "delegator/1-my-ticket", testAgentID)
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

func TestCancelWithNoSuchTicket(t *testing.T) {
	s, _ := oneTicket(t)
	if err := s.Cancel(404, 0); !errors.Is(err, ErrNoTicket) {
		t.Errorf("err = %v, want ErrNoTicket", err)
	}
}

func TestFinishTicketOnARunningTicket(t *testing.T) {
	s, _ := threeReady(t)
	fourth, err := s.AddTicket(mustProject(t, s), "fourth")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(fourth, "delegator/4-fourth", testAgentID); err != nil {
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

// Work that failed under the supervisor can be completed by continuing its
// session with dg chat. Finishing that work records the commit, appends the
// ticket to READY, and records the direct failed-to-ready change in history.
func TestFinishTicketOnAFailedTicket(t *testing.T) {
	s, _ := threeReady(t)
	fourth, err := s.AddTicket(mustProject(t, s), "fourth")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(fourth, "delegator/4-fourth", testAgentID); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeStatus(fourth, Failed); err != nil {
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
	wantSteps := []string{"new to queued", "queued to running", "running to failed", "failed to ready"}
	if got := steps(t, s, fourth); !slices.Equal(got, wantSteps) {
		t.Errorf("the history is\n%v\nwant\n%v", got, wantSteps)
	}
}

// A repeated finish updates an amended commit without changing ready position
// or transition history.
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

func TestFinishTicketFromAnInvalidState(t *testing.T) {
	for _, status := range []TicketStatus{Queued, Done, Cancelled} {
		t.Run(string(status), func(t *testing.T) {
			s, id := oneTicket(t)
			if status != Queued {
				setStatus(t, s, id, status)
			}

			err := s.FinishTicket(id, "abc1234")
			if !errors.Is(err, ErrInvalidTicketStateChange) {
				t.Fatalf("err = %v, want ErrInvalidTicketStateChange", err)
			}

			ticket, err := s.Ticket(id)
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

// AllTickets includes closed work even after it leaves the inbox's DONE
// window.
func TestAllTicketsGivesEveryTicketWhateverItsStatus(t *testing.T) {
	s, projectID := emptyStore(t)
	want := map[int64]TicketStatus{}
	var done int64
	for _, status := range []TicketStatus{Queued, Running, Ready, Failed, Done, Cancelled} {
		id, err := s.AddTicket(projectID, string(status))
		if err != nil {
			t.Fatal(err)
		}
		if status != Queued {
			setStatus(t, s, id, status)
		}
		want[id] = status
		if status == Done {
			done = id
		}
	}
	// Backdate acceptance beyond the DONE window.
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

// Project filtering belongs in the query so unrelated rows are not loaded.
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

// An unknown project path returns an empty list, not an error.
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
