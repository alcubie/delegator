// The migration of the database of delegator. Each step is one element of
// migrations, and PRAGMA user_version holds the count of steps that the
// database has. A new version of delegator adds a step at the end of the list
// and never changes a step that a person has.

package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// ErrNewerDatabase shows that a later version of delegator made the database.
// The person must install that version again, because this one does not know
// each step that made the database what it is.
var ErrNewerDatabase = errors.New("the database comes from a later version of delegator")

// migrations holds one step for each version of the database. The number of a
// step is its position in the list, and the first step is version 1. A step
// that a person installed already must never change: the way to change the
// database is a new step at the end of the list.
//
// Lesson 3 of the technical document says that CREATE TABLE IF NOT EXISTS is
// not a migration. This list is the answer to that lesson.
var migrations = []string{
	tables,
	completedColumn,
	dropTicketsColumns,
	addTicketsCommitColumn,
}

// tables makes the two tables and the index of the queue. The ids of tickets
// are unique for all projects, so dg show 4 is not ambiguous.
//
// The CHECK keeps the status and the position together: a ticket is in the queue
// with both, and with neither half alone. A unique index counts each NULL as
// different from each other NULL, so it does not affect a ticket that is out of
// the queue.
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
  status     TEXT NOT NULL,
  position   INTEGER,
  branch     TEXT,
  session    TEXT,
  result     TEXT,
  flags      TEXT,
  created    TEXT NOT NULL,
  CHECK ((status = 'queued') = (position IS NOT NULL))
);

CREATE UNIQUE INDEX tickets_position ON tickets(position);
`

// READY is in the order of the time of completion, and a ticket that becomes
// ready goes at the end. The order of the queue is not stable for READY: a
// slow ticket that entered the queue first arrives above the tickets that the
// person can see now, so the list moves below the eyes of the person.
//
// A ticket that never became ready holds NULL, and a ticket that goes back to
// the queue and becomes ready again holds the later time.
const completedColumn = `
ALTER TABLE tickets ADD COLUMN completed TEXT;
`

const dropTicketsColumns = `
ALTER TABLE tickets DROP COLUMN result;
ALTER TABLE tickets DROP COLUMN flags;
`

const addTicketsCommitColumn = `
ALTER TABLE tickets ADD COLUMN commit_id TEXT;
`

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
