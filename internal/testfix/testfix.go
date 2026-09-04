// Package testfix holds the fixtures that the tests of more than one package
// need: a repository, a store, a queued ticket, the fake agent, and a launch a
// test can watch. Each was copied between the cli, run and adapters tests
// before this package existed.
//
// It imports testing, which a package that is not a test normally does not.
// The alternative was the copies. Only a test binary will ever import it.
//
// It must not import adapters: the adapters tests import this package, and an
// internal test package cannot be on a cycle. The tests of project and store
// keep their own helpers for the same reason and one more: Open and the git
// commands are what those tests examine.
package testfix

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// RecordingLaunch returns a launch that writes the id it was given to a file,
// and the path of that file. It stands in for the launch of dg run, so a test
// sees which ticket was chosen without starting a real run.
func RecordingLaunch(t *testing.T) (func(id int64) *exec.Cmd, string) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "started")
	return func(id int64) *exec.Cmd {
		return exec.Command("sh", "-c", `echo "$1" > "$2"`, "--", fmt.Sprint(id), marker)
	}, marker
}

// WaitFor returns the content of path once it exists, or fails the test after
// a short wait. A launched program is not waited on, so the test has to.
func WaitFor(t *testing.T, path string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
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

// RunTests builds dg-fake-agent once for a package and runs its tests, and
// builds dg beside it when withDG is set, for a fake agent whose script calls
// dg finish. It is called from TestMain and returns the code to exit with:
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
