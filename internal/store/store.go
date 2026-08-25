// Package store keeps each field of delegator in one SQLite database. The
// prose of a ticket is not here: the person writes the prose with an editor,
// and an editor opens a file and not a row.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	// The blank name imports this package for its init function only. The init
	// function puts the driver in the registry of database/sql below the name
	// "sqlite", and each call of this package goes through database/sql.
	_ "modernc.org/sqlite"
)

// ErrNewerDatabase shows that a later version of delegator made the database.
// The person must install that version again, because this one does not know
// each step that made the database what it is.
var ErrNewerDatabase = errors.New("the database comes from a later version of delegator")

// dbFile is the name of the database below the data directory. A change of
// this name loses the data of each person who has delegator now.
const dbFile = "delegator.db"

// dirPerm gives the permission of the data directory and of each directory
// below it. A ticket can contain private data, and a directory with no
// permission for a group or for other persons stops each other person on the
// computer. Delegator therefore gives the permission in this one place, and no
// command gives a permission to the file that it writes.
const dirPerm = 0o700

// dataDirs are the directories that hold the files. The prose of each ticket is
// in tickets, the copy of a repository for each run is in worktrees, and the
// log of each agent is in runs.
var dataDirs = []string{"", "tickets", "worktrees", "runs"}

// busyTimeout is the time in milliseconds that a connection waits for the lock
// of a writer before it gives an error. Delegator has no server, so two
// commands of the person, or a command and a supervisor, can want the lock at
// the same time. A wait is correct here, and an error is not.
const busyTimeout = 5000

// dsn gives the name that sql.Open takes. Each setting is in the name, and not
// in a statement after the open, because database/sql keeps a pool of
// connections and makes a new one at any time. A statement after the open
// reaches one connection only.
//
// The path goes in a URL, so a path that holds ? or # needs an escape. Without
// it SQLite reads the path only as far as that character, and it makes the
// database at a different place and gives no error. url.URL escapes the path
// and keeps each separator of a directory.
func dsn(dataDir string) string {
	u := url.URL{
		Scheme: "file",
		Path:   filepath.Join(dataDir, dbFile),
		RawQuery: fmt.Sprintf("_pragma=journal_mode(WAL)&_pragma=busy_timeout(%d)",
			busyTimeout),
	}
	return u.String()
}

// Store holds the open database. Each command makes one Store, and closes it
// when the command stops.
type Store struct {
	db *sql.DB
}

// migrations holds one step for each version of the database. The number of a
// step is its position in the list, and the first step is version 1. A step
// that a person installed already must never change: the way to change the
// database is a new step at the end of the list.
//
// Lesson 3 of the technical document says that CREATE TABLE IF NOT EXISTS is
// not a migration. This list is the answer to that lesson.
var migrations = []string{tables}

// tables makes the two tables. The ids of tickets are one sequence for all
// projects, so INTEGER PRIMARY KEY gives the number and no counter is
// necessary. The column position is the queue, and a ticket with a position of
// NULL is not in the queue. The path of a worktree is worktrees/<id>, so it
// needs no column. The branch keeps a column, because the person can give the
// branch a new name.
const tables = `
CREATE TABLE projects (
  id             INTEGER PRIMARY KEY,
  path           TEXT NOT NULL UNIQUE,
  default_branch TEXT NOT NULL,
  first_commit   TEXT
);

CREATE TABLE tickets (
  id         INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects(id),
  title      TEXT NOT NULL,
  state      TEXT NOT NULL,
  position   INTEGER,
  branch     TEXT,
  session    TEXT,
  result     TEXT,
  flags      TEXT,
  created    TEXT NOT NULL
);
`

// Open gives the database below dataDir. It makes the data directory, the
// directories below it, and the database, if they are not present.
func Open(dataDir string) (*Store, error) {
	// The directories come first. SQLite cannot make its file below a directory
	// that is not present.
	for _, name := range dataDirs {
		if err := os.MkdirAll(filepath.Join(dataDir, name), dirPerm); err != nil {
			return nil, err
		}
	}

	db, err := sql.Open("sqlite", dsn(dataDir))
	if err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close stops the connection to the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// migrate applies each step above the number in PRAGMA user_version, and then
// writes the new number. A database that is current gets no statement.
func migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}

	// A number above the last step means that a later version of delegator made
	// this database. This version does not know what that step did, so it stops
	// and writes nothing.
	if version > len(migrations) {
		return fmt.Errorf("%w: the database is version %d, and this delegator knows version %d",
			ErrNewerDatabase, version, len(migrations))
	}

	for i := version; i < len(migrations); i++ {
		if err := applyStep(db, i); err != nil {
			return fmt.Errorf("migration step %d: %w", i+1, err)
		}
	}
	return nil
}

// applyStep applies one step and its new number below one transaction. A step
// that gives an error part way therefore leaves the database as it was.
func applyStep(db *sql.DB, i int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	// A rollback after a commit gives sql.ErrTxDone, and this discards it. The
	// rollback operates only when a statement below gives an error.
	defer tx.Rollback()

	if _, err := tx.Exec(migrations[i]); err != nil {
		return err
	}
	// PRAGMA takes no parameter, so the number goes in the text of the
	// statement. The number is the position in a list of this package, and no
	// text of a person reaches here.
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
		return err
	}
	return tx.Commit()
}
