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
	addQueueTable,
	addRunsTable,
	addReadyPositionColumn,
	addDependenciesTable,
	addTransitionsTable,
	dropCompletedColumn,
	dropCreatedColumn,
	addAgentsTable,
	addAgentRegistry,
	verifyGooseACP,
	verifyGitHubCopilotACP,
	addConfigurationSettings,
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

// completedColumn holds the time that a ticket became ready. DONE comes in the
// order of it, dg show says how long ago the run stopped, and the window of
// DONE reads it to decide which tickets it still shows.
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

const addQueueTable = `
CREATE TABLE queue_state (
 id      INTEGER PRIMARY KEY CHECK (id = 1),
 running BOOLEAN NOT NULL DEFAULT 1 CHECK (running IN (0, 1))
);
INSERT INTO queue_state (id, running) VALUES (1, 1);
`

// addRunsTable makes the table of runs. A ticket has more than one run after
// dg restart and dg revise, and the process id, the start time, the end time
// and the exit code are facts of one run, so they are a row here and not a
// column of tickets. A run that has not ended holds NULL in ended_at and
// exit_code.
const addRunsTable = `
CREATE TABLE runs (
  id         INTEGER PRIMARY KEY,
  ticket_id  INTEGER NOT NULL REFERENCES tickets(id),
  pid        INTEGER,
  started_at TEXT NOT NULL,
  ended_at   TEXT,
  exit_code  INTEGER
);
`

// addReadyPositionColumn gives READY an order that the person sets, so a later
// ticket can go to the top of the tickets that wait for review. A ticket that
// becomes ready takes the end of READY, which is where the order of completion
// put it before this column, and dg move takes it from there.
//
// The column is not position: the CHECK of the table holds that one to a ticket
// of the queue, and the place of a ticket in READY is a different place.
//
// The UPDATE gives each ready ticket of a database that migrates the place it
// has now, which is the order of completion with the id between two tickets of
// one second, so READY stays as the person last saw it. A ready ticket with no
// completed time comes first, as it did in that order.
const addReadyPositionColumn = `
ALTER TABLE tickets ADD COLUMN ready_position INTEGER;

UPDATE tickets SET ready_position = (
  SELECT COUNT(*) FROM tickets AS earlier
  WHERE earlier.status = 'ready'
    AND (COALESCE(earlier.completed, '') < COALESCE(tickets.completed, '')
      OR (COALESCE(earlier.completed, '') = COALESCE(tickets.completed, '')
          AND earlier.id <= tickets.id))
) WHERE status = 'ready';

CREATE UNIQUE INDEX tickets_ready_position ON tickets(ready_position);
`

// addDependenciesTable makes the table of links between tickets. One row says
// that the ticket ticket_id depends on the ticket depends_on, so the row for
// "ticket 3 must be done before ticket 5" is (5, 3).
//
// The link is a row and not a column, because a ticket can depend on more than
// one other ticket. The primary key is the pair, so a link that is written
// twice is one row, and the CHECK refuses a ticket that depends on itself. The
// index on depends_on is for the other direction of the question: which
// tickets depend on this one.
const addDependenciesTable = `
CREATE TABLE ticket_deps (
  ticket_id  INTEGER NOT NULL REFERENCES tickets(id),
  depends_on INTEGER NOT NULL REFERENCES tickets(id),
  PRIMARY KEY (ticket_id, depends_on),
  CHECK (ticket_id <> depends_on)
) WITHOUT ROWID;

CREATE INDEX ticket_deps_depends_on ON ticket_deps(depends_on);
`

// addTransitionsTable makes the table of the history of each ticket. One row is
// one change of state: the ticket, the status it left, the status it entered
// and the time. The first row of a ticket is its arrival, which has no status
// before it, and the last row is the status the ticket has now.
//
// A column that holds one event goes out of date, which the column completed
// showed: dg revise takes a ticket back to the queue and that column still
// holds the time of the run that is complete, so the queue showed the time
// that an earlier run stopped. A row is written once and never again, so no
// time here can say a thing that was true on another day.
//
// The two INSERTs give a database that migrates the history it holds. The
// arrival of each ticket is the column created. The time that a ticket became
// ready is the column completed, and the row goes in for a ticket that is in
// ready or in done, because that ticket is still where the time put it. A
// ticket that left ready has a later change whose time the database does not
// hold, and a row for the earlier one would then read as the last change of
// that ticket. FEATURES.md says that you cannot get this data later: the
// history of each ticket begins with the times that are there.
const addTransitionsTable = `
CREATE TABLE transitions (
  id          INTEGER PRIMARY KEY,
  ticket_id   INTEGER NOT NULL REFERENCES tickets(id),
  from_status TEXT,
  to_status   TEXT NOT NULL,
  at          TEXT NOT NULL
);

CREATE INDEX transitions_ticket ON transitions(ticket_id, id);

INSERT INTO transitions (ticket_id, from_status, to_status, at)
  SELECT id, NULL, 'queued', created FROM tickets;

INSERT INTO transitions (ticket_id, from_status, to_status, at)
  SELECT id, 'running', 'ready', completed FROM tickets
  WHERE completed IS NOT NULL AND status IN ('ready', 'done');
`

// dropCompletedColumn takes away the column that held the time of one change of
// state: the last change into ready, which the table transitions holds now.
//
// The column went out of date, because a ticket that leaves ready keeps the
// time it had. A ticket that went back to the queue held the time that its
// earlier run stopped, and dg show wrote that time below the status queued.
const dropCompletedColumn = `
ALTER TABLE tickets DROP COLUMN completed;
`

// dropCreatedColumn takes away the column that held the arrival of a ticket,
// which is the first row of its history: the row with no status before it.
//
// The step comes after the one that makes transitions, because that one reads
// created to seed the history of each ticket that the database already holds.
const dropCreatedColumn = `
ALTER TABLE tickets DROP COLUMN created;
`

const addAgentsTable = `
CREATE TABLE agents (
  id   INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE
);

INSERT INTO agents (name) VALUES ('claude'), ('codex'), ('gemini');

ALTER TABLE runs ADD COLUMN agent_id INTEGER REFERENCES agents(id);
`

// addAgentRegistry makes the database the source of truth for runnable
// agents. JSON keeps argv as one value while preserving argument boundaries.
const addAgentRegistry = `
ALTER TABLE agents ADD COLUMN argv TEXT NOT NULL DEFAULT '[]';
ALTER TABLE agents ADD COLUMN resume_argv TEXT NOT NULL DEFAULT '[]';
ALTER TABLE agents ADD COLUMN install_hint TEXT;

UPDATE agents SET
  argv = CASE name
    WHEN 'claude' THEN '["claude-agent-acp"]'
    WHEN 'codex' THEN '["codex-acp"]'
    WHEN 'gemini' THEN '["gemini","--experimental-acp"]'
    ELSE '[]' END,
  resume_argv = CASE name
    WHEN 'claude' THEN '["claude","--resume","{session}"]'
    WHEN 'codex' THEN '["codex","resume","{session}"]'
    ELSE '[]' END;

INSERT INTO agents (name, argv, resume_argv, install_hint) VALUES
  ('opencode', '["opencode","acp"]', '["opencode","--session","{session}"]', 'Install OpenCode'),
  ('goose', '["goose","acp"]', '[]', 'Install Goose'),
  ('github-copilot', '["copilot","--acp"]', '[]', 'Install GitHub Copilot CLI'),
  ('cursor', '["cursor-agent","acp"]', '["cursor-agent","--resume","{session}"]', 'Install Cursor Agent'),
  ('pi', '["pi","--acp"]', '[]', 'Install Pi');

CREATE TABLE settings (
  id                   INTEGER PRIMARY KEY CHECK (id = 1),
  runs                 INTEGER NOT NULL CHECK (runs >= 1),
  timeout_minutes      INTEGER NOT NULL CHECK (timeout_minutes >= 1),
  done_hours           INTEGER NOT NULL CHECK (done_hours >= 0),
  max_runs_per_project INTEGER NOT NULL CHECK (max_runs_per_project >= 0),
  default_agent_id     INTEGER REFERENCES agents(id)
) STRICT;
`

// verifyGooseACP records the commands verified with Goose 1.50.1. Goose
// serves ACP over stdio with "goose acp", while its terminal client resumes
// a session by its ACP id through "goose session --resume --session-id".
const verifyGooseACP = `
UPDATE agents SET
  argv = '["goose","acp"]',
  resume_argv = '["goose","session","--resume","--session-id","{session}"]',
  install_hint = 'Install Goose 1.50.1 or later'
WHERE name = 'goose';
`

// verifyGitHubCopilotACP records the commands verified with GitHub Copilot
// CLI 1.0.85. The same executable serves ACP over stdio and resumes its saved
// session in the terminal.
const verifyGitHubCopilotACP = `
UPDATE agents SET
  argv = '["copilot","--acp"]',
  resume_argv = '["copilot","--resume={session}"]',
  install_hint = 'Install GitHub Copilot CLI 1.0.85 or later'
WHERE name = 'github-copilot';
`

// addConfigurationSettings seeds the one instance-settings row with the
// defaults used before SQLite became their authority.
const addConfigurationSettings = `
INSERT INTO settings
  (id, runs, timeout_minutes, done_hours, max_runs_per_project)
VALUES (1, 2, 60, 24, 0);
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
