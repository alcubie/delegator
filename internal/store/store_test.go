package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
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

	// The literal 1 is the count of steps that this version has. A test that
	// read len(migrations) would stay green after a new step, because that one
	// change moves the value that the test wants at the same time.
	if got := userVersion(t, s.db); got != 1 {
		t.Errorf("user_version = %d, want 1", got)
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

	if got := userVersion(t, second.db); got != 1 {
		t.Errorf("user_version = %d, want 1", got)
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
	if got := userVersion(t, db); got != 1 {
		t.Errorf("user_version = %d, want 1", got)
	}
	var name string
	err = db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'later'").Scan(&name)
	if err == nil {
		t.Error("the table later stayed after the step gave an error")
	}
}
