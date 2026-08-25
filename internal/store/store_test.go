package store

import (
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
