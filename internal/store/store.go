// Package store keeps each field of delegator in one SQLite database. The
// prose of a ticket is not here: the person writes the prose with an editor,
// and an editor opens a file and not a row.
package store

import (
	"database/sql"
	"os"
	"path/filepath"

	// The blank name imports this package for its init function only. The init
	// function puts the driver in the registry of database/sql below the name
	// "sqlite", and each call of this package goes through database/sql.
	_ "modernc.org/sqlite"
)

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

// Store holds the open database. Each command makes one Store, and closes it
// when the command stops.
type Store struct {
	db *sql.DB
}

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

	db, err := sql.Open("sqlite", filepath.Join(dataDir, dbFile))
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(tables); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close stops the connection to the database.
func (s *Store) Close() error {
	return s.db.Close()
}
