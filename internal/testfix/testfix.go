// Package testfix holds the fixtures that the tests of more than one package
// need: a repository, a store, a queued ticket, the fake agent, and a launch a
// test can watch. Each was copied between the cli and run tests
// before this package existed.
//
// It imports testing, which a package that is not a test normally does not.
// The alternative was the copies. Only a test binary will ever import it.
//
// The tests of project and store keep their own helpers because Open and the
// git commands are what those tests examine.
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

// GitIn runs one git command in dir. It stops the test if git gives an error,
// because a repository the test cannot build is not a result of the test.
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

// CommitIn makes one empty commit. The identity is in the command, so the test
// does not read the config of the person who runs it.
func CommitIn(t *testing.T, dir, message string) {
	t.Helper()
	GitIn(t, dir, "-c", "user.email=test@example.com", "-c", "user.name=Test",
		"commit", "--allow-empty", "-q", "-m", message)
}

// Repo makes an empty repository on the named branch. The name is given so a
// test does not depend on the git config of the person who runs it.
func Repo(t *testing.T, branch string) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := project.Command(dir, "init", "-q", "-b", branch).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	return dir
}

// OpenStore opens the store of a data directory and closes it when the test
// ends, which is the defer a test helper cannot do for its caller.
func OpenStore(t *testing.T, dataDir string) *store.Store {
	t.Helper()
	s, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
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

// FreePID returns a process id that no program holds, which is what the
// reconcile sees for a supervisor that stopped. A child that has run and been
// collected leaves its id free, and the system gives that id again only after
// many thousands of other programs.
func FreePID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

// Group starts sh with script in a session of its own and returns the id of
// the process group it leads. A script that starts a child of its own puts
// that child in the same group, so one group stands for a supervisor with an
// agent below it, and a test can stop the group and see that each program of
// it ended.
//
// The script gets the path of a marker file as $1, and must create the marker
// once it has done everything a signal has to find: its trap set, its child
// started. Group returns after the marker is there. Without that the test
// races the shell reading its own script, and a signal that arrives first
// reaches a shell with no trap and no child.
//
// The shell is a child of the test, so a goroutine waits on it: a child that
// nobody collects stays as a zombie, and a zombie still answers a signal, so
// the group would never look gone. The group is killed when the test ends, for
// a test whose own work leaves it alive.
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

// supervisorScript is the script of a Group that stands for a supervisor with
// an agent below it: one shell and one child of the shell, both in the group,
// and neither of them keeping a signal.
const supervisorScript = `sleep 60 & : > "$1"; sleep 60`

// GroupAlive reports whether a process group still holds a program. Signal 0
// sends nothing, and to a group it gives ESRCH only when the group holds no
// program at all, so one call answers for the supervisor and for each program
// below it together.
func GroupAlive(pgid int) bool {
	return !errors.Is(syscall.Kill(-pgid, 0), syscall.ESRCH)
}

// WaitForGroupGone fails the test if the group still holds a program after a
// short wait. A signal is delivered while the program that sent it continues,
// so the test waits for the programs to go rather than reading once.
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

// LiveRun makes the last run of a ticket look like the run of a supervisor
// that is working, by starting a program in a session of its own and putting
// its process id on the row. The program starts a child, which stands for the
// agent below a supervisor. It returns the process group that holds both.
func LiveRun(t *testing.T, dataDir string, ticketID int64) int {
	t.Helper()
	pgid := Group(t, supervisorScript)
	setRunColumn(t, dataDir, ticketID, "pid", pgid)
	return pgid
}

// StaleRun makes the last run of a ticket look like the run of a supervisor
// that is gone, by putting a process id that no program holds on its row.
func StaleRun(t *testing.T, dataDir string, ticketID int64) {
	t.Helper()
	setRunColumn(t, dataDir, ticketID, "pid", FreePID(t))
}

// AgeRun moves the start of the last run of a ticket that far into the past,
// which is how a test reaches the timeout of a run without waiting for it.
func AgeRun(t *testing.T, dataDir string, ticketID int64, age time.Duration) {
	t.Helper()
	started := time.Now().Add(-age).UTC().Format(time.RFC3339)
	setRunColumn(t, dataDir, ticketID, "started_at", started)
}

// AgeAcceptance moves the moment that the person accepted a ticket back by
// age. The window of DONE reads that moment, and a test cannot wait a day for
// the window to pass a ticket, so it writes the row of the history itself.
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

// setRunColumn writes one column of the last run of a ticket. It opens the
// database itself, because no command can leave a run in the states above: a
// claim writes the moment it happens and the id of the program that claims,
// which in a test is the test, and the test can neither wait an hour nor crash
// to make its own id free.
func setRunColumn(t *testing.T, dataDir string, ticketID int64, column string, value any) {
	t.Helper()
	db := openDB(t, dataDir)
	// The name of the column is this file's and never a test's, so it is safe
	// in the text of the statement, where SQLite takes no parameter.
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

// openDB opens the database of a data directory and closes it when the test
// ends. The name of the file is the store's, and the store gives no way to
// reach the database it holds, so a fixture that reaches a column no command
// reaches opens it again.
func openDB(t *testing.T, dataDir string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "delegator.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// XDGDataDir points XDG_DATA_HOME at a fresh directory for one test and
// returns the data directory dg will use below it, so that a dg the fake agent
// runs reaches this test's database and not the person's.
func XDGDataDir(t *testing.T) string {
	t.Helper()
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	return filepath.Join(xdg, "delegator")
}

// XDGConfigDir points XDG_CONFIG_HOME at a fresh directory for one test and
// returns the config directory dg will use below it, so that a test of the
// config file reads and writes its own file and not the person's.
func XDGConfigDir(t *testing.T) string {
	t.Helper()
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
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

// RecordingLaunch returns a launch that writes a file, and the path of that
// file. It stands in for the launch of dg run, so a test sees that a
// supervisor was started without starting a real run. The supervisor takes the
// ticket for itself, so there is no ticket for the launch to record.
//
// Each launch appends a line, because a trigger starts one supervisor for each
// free slot and a test of a limit above one counts them.
func RecordingLaunch(t *testing.T) (func() *exec.Cmd, string) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "started")
	return func() *exec.Cmd {
		return exec.Command("sh", "-c", `echo started >> "$1"`, "--", marker)
	}, marker
}

// waitTimeout is how long a wait gives a launched program to write its marker.
// The programs are shells that write a file and take milliseconds, so the
// timeout is the cost of a test that fails and not a time a test that passes
// ever spends.
const waitTimeout = 2 * time.Second

// settle is how long a wait for a launch waits after it has what it wants, to
// catch a launch that should not have happened. A launched program writes its
// marker in less than this on the computers that run the suite.
const settle = 50 * time.Millisecond

// WaitForStarts fails the test unless exactly want supervisors were started
// and recorded at marker, the file of RecordingLaunch. It waits for that many
// and then waits again, because the fault it has to catch is one supervisor
// too many as much as one too few, and a launch that nothing waits on arrives
// when it arrives.
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

// failing is what a wait needs of the test it fails. The test of a wait that
// fails passes a stand-in, because a real *testing.T would fail with it and
// Fatalf on a real one does not return.
type failing interface {
	Helper()
	Fatalf(format string, args ...any)
}

// WaitFor returns the content of path once it exists, or fails the test after
// a short wait. A launched program is not waited on, so the test has to.
func WaitFor(t failing, path string) string {
	t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil {
			return strings.TrimSpace(string(data))
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s was not written", path)
	return ""
}

// FakeAgentPath is dg-fake-agent, built by RunTests once for the package.
var FakeAgentPath string

// RunTests builds the fake agent once for a package and runs its tests, and
// builds dg beside them when withDG is set, for a fake agent whose script
// calls dg finish. It is called from TestMain and returns the code to exit with:
// os.Exit runs no deferred call, so the cleanup lives here and not there.
//
// It also points XDG_CONFIG_HOME below its directory for the whole package.
// Each command of dg writes config.toml when the file is not there, so every
// test that runs a command would otherwise write below ~/.config of the
// person. A test of the config file sets its own directory with XDGConfigDir.
//
// Go has no setup that spans packages. Each package's tests are their own
// process, so each builds its own copy; this only stops the builds drifting.
// The directory is unique because go test runs packages side by side.
func RunTests(m *testing.M, withDG bool) int {
	dir, err := os.MkdirTemp("", "delegator-testfix")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(dir)
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))

	FakeAgentPath = filepath.Join(dir, "dg-fake-agent")
	if err := build(FakeAgentPath, "github.com/alcubie/delegator/cmd/dg-fake-agent"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if withDG {
		if err := build(filepath.Join(dir, "dg"), "github.com/alcubie/delegator/cmd/dg"); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		os.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	return m.Run()
}

// build compiles one command of this module to path.
func build(path, pkg string) error {
	if out, err := exec.Command("go", "build", "-o", path, pkg).CombinedOutput(); err != nil {
		return fmt.Errorf("go build %s: %w: %s", pkg, err, out)
	}
	return nil
}
