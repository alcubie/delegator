// Package testfix provides shared repository, store, agent, and process
// fixtures. It is used only by tests. The project and store packages keep
// local fixtures for the operations they test directly.
package testfix

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/store"
)

// GitIn runs Git in dir and fails the test on error.
func GitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := project.Command(dir, args...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

// GitOut runs one git command in dir and returns its output with no final
// newline.
func GitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := project.Command(dir, args...).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("git %v: %v: %s", args, err, exitErr.Stderr)
		}
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// CommitIn creates an empty commit with an explicit identity, independent of
// the user's Git configuration.
func CommitIn(t *testing.T, dir, message string) {
	t.Helper()
	GitIn(t, dir, "-c", "user.email=test@example.com", "-c", "user.name=Test",
		"commit", "--allow-empty", "-q", "-m", message)
}

// Repo creates an empty repository with an explicit initial branch,
// independent of the user's Git configuration.
func Repo(t *testing.T, branch string) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := project.Command(dir, "init", "-q", "-b", branch).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	return dir
}

// OpenStore opens the store and registers cleanup at the end of the test.
func OpenStore(t *testing.T, dataDir string) *store.Store {
	t.Helper()
	s, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// UseAgent saves agent as the default registry entry for one test database.
func UseAgent(t *testing.T, dataDir, name string, argv ...string) {
	t.Helper()
	s := OpenStore(t, dataDir)
	if err := s.SaveAgent(store.Agent{Name: name, Argv: argv}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDefaultAgent(name); err != nil {
		t.Fatal(err)
	}
}

// ReadTicket returns one ticket from the data directory.
func ReadTicket(t *testing.T, dataDir string, id int64) store.Ticket {
	t.Helper()
	ticket, err := OpenStore(t, dataDir).Ticket(id)
	if err != nil {
		t.Fatal(err)
	}
	return ticket
}

// Dependencies returns the id of each ticket that the ticket id depends on, in
// the order of the id.
func Dependencies(t *testing.T, dataDir string, id int64) []int64 {
	t.Helper()
	ids, err := OpenStore(t, dataDir).Dependencies(id)
	if err != nil {
		t.Fatal(err)
	}
	return ids
}

// SecondTicket adds one more ticket to the queue of a data directory that
// already holds a project, and returns its id.
func SecondTicket(t *testing.T, dataDir string) int64 {
	t.Helper()
	s := OpenStore(t, dataDir)
	projects, err := s.Projects()
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.AddTicket(projects[0].ID, "the second")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// FreePID returns the PID of a child that has exited and been reaped. Tests
// use it to represent a dead supervisor; immediate PID reuse is unlikely.
func FreePID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

// Group starts a shell in a new session and returns its process-group ID.
// Child processes in that group stand in for an agent under a supervisor.
//
// The script receives a marker path as $1 and must create it after installing
// traps and starting children. Waiting for the marker prevents signals from
// racing setup. A goroutine reaps the shell so zombies do not keep the group
// apparently alive. Cleanup kills any remaining group members.
func Group(t *testing.T, script string) int {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "ready")
	cmd := exec.Command("sh", "-c", script, "--", marker)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go cmd.Wait()

	pgid := cmd.Process.Pid
	t.Cleanup(func() { syscall.Kill(-pgid, syscall.SIGKILL) })
	WaitFor(t, marker)
	return pgid
}

// supervisorScript creates a shell and child in one group, neither trapping
// termination signals.
const supervisorScript = `sleep 60 & : > "$1"; sleep 60`

// GroupAlive probes the group with signal 0. ESRCH means no members remain.
func GroupAlive(pgid int) bool {
	return !errors.Is(syscall.Kill(-pgid, 0), syscall.ESRCH)
}

// WaitForGroupGone waits briefly for asynchronous signal delivery and process
// exit, then fails if the group remains alive.
func WaitForGroupGone(t *testing.T, pgid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !GroupAlive(pgid) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("the process group %d still holds a program", pgid)
}

// LiveRun gives the latest run a live supervisor PID. It starts a shell with
// a child representing the agent and returns their process-group ID.
func LiveRun(t *testing.T, dataDir string, ticketID int64) int {
	t.Helper()
	pgid := Group(t, supervisorScript)
	setRunColumn(t, dataDir, ticketID, "pid", pgid)
	return pgid
}

// StaleRun replaces the latest run's PID with one whose process has exited.
func StaleRun(t *testing.T, dataDir string, ticketID int64) {
	t.Helper()
	setRunColumn(t, dataDir, ticketID, "pid", FreePID(t))
}

// AgeRun backdates the latest run to test timeouts without waiting.
func AgeRun(t *testing.T, dataDir string, ticketID int64, age time.Duration) {
	t.Helper()
	started := time.Now().Add(-age).UTC().Format(time.RFC3339)
	setRunColumn(t, dataDir, ticketID, "started_at", started)
}

// AgeAcceptance backdates the acceptance transition to test the DONE window
// without waiting.
func AgeAcceptance(t *testing.T, dataDir string, ticketID int64, age time.Duration) {
	t.Helper()
	at := time.Now().Add(-age).UTC().Format(time.RFC3339)
	result, err := openDB(t, dataDir).Exec(`
		UPDATE transitions SET at = ?
		WHERE ticket_id = ? AND to_status = 'done'`, at, ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		t.Fatalf("the acceptance of %d rows of ticket %d was written, want 1 row: %v", n, ticketID, err)
	}
}

// setRunColumn edits the latest run directly to create states unavailable
// through normal commands, such as an old start time or a dead supervisor
// PID.
func setRunColumn(t *testing.T, dataDir string, ticketID int64, column string, value any) {
	t.Helper()
	db := openDB(t, dataDir)
	// The column name comes from this file, not test input; SQL
	// identifiers cannot use parameters.
	result, err := db.Exec(fmt.Sprintf(`
		UPDATE runs SET %s = ?
		WHERE id = (SELECT id FROM runs WHERE ticket_id = ? ORDER BY id DESC LIMIT 1)`, column),
		value, ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		t.Fatalf("the %s of %d rows of ticket %d was written, want 1 row: %v", column, n, ticketID, err)
	}
}

// openDB opens a separate connection for fixture-only database changes and
// registers cleanup. Store intentionally exposes no raw database connection.
func openDB(t *testing.T, dataDir string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "delegator.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// XDGDataDir sets XDG_DATA_HOME to a fresh directory and returns dg's data
// path, isolating commands launched by the fake agent from the user's
// database.
func XDGDataDir(t *testing.T) string {
	t.Helper()
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	return filepath.Join(xdg, "delegator")
}

// Script writes the lines of a fake agent script to a file and returns its
// path.
func Script(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "script")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// RecordingLaunch returns a supervisor command that appends to a marker file,
// plus the marker path. Tests count launches without starting real
// supervisors. No ticket ID is recorded because supervisors select their own
// tickets.
func RecordingLaunch(t *testing.T) (func() *exec.Cmd, string) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "started")
	return func() *exec.Cmd {
		return exec.Command("sh", "-c", `echo started >> "$1"`, "--", marker)
	}, marker
}

// waitTimeout bounds how long tests wait for subprocess markers. Successful
// waits return as soon as the marker appears.
const waitTimeout = 2 * time.Second

// settle is the extra wait used to detect unexpected launches after the
// expected count has arrived.
const settle = 50 * time.Millisecond

// WaitForStarts waits for exactly want launch records, then waits briefly for
// unexpected extra launches. It fails on either too few or too many.
func WaitForStarts(t *testing.T, marker string, want int) {
	t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for starts(t, marker) < want && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(settle)
	if got := starts(t, marker); got != want {
		t.Errorf("%d supervisors were started, want %d", got, want)
	}
}

// starts returns how many supervisors the recording launch has written.
func starts(t *testing.T, marker string) int {
	t.Helper()
	data, err := os.ReadFile(marker)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return len(strings.Fields(string(data)))
}

// failing lets wait-helper tests substitute a recorder for testing.T so
// expected failures do not fail the enclosing test.
type failing interface {
	Helper()
	Fatalf(format string, args ...any)
}

type waitClock interface {
	Now() time.Time
	Sleep(time.Duration)
}

type realWaitClock struct{}

func (realWaitClock) Now() time.Time { return time.Now() }

func (realWaitClock) Sleep(duration time.Duration) { time.Sleep(duration) }

// WaitFor waits for path to exist and returns its contents, failing after a
// short timeout.
func WaitFor(t failing, path string) string {
	return waitFor(t, path, waitTimeout, realWaitClock{})
}

func waitFor(t failing, path string, timeout time.Duration, clock waitClock) string {
	t.Helper()
	deadline := clock.Now().Add(timeout)
	for clock.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil {
			return strings.TrimSpace(string(data))
		}
		clock.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s was not written", path)
	return ""
}
