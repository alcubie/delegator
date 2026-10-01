// Database migrations are append-only. PRAGMA user_version records how many
// steps have been applied.

package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// ErrNewerDatabase means this database requires a newer version of delegator.
var ErrNewerDatabase = errors.New("the database comes from a later version of delegator")

// migrations holds one step for each version of the database, starting at 1.
// The pre-release migrations were squashed into initialSchema. Future changes
// must append a step rather than change one that has already shipped.
var migrations = []string{
	initialSchema,
	analyticsConsentSchema,
	`ALTER TABLE settings RENAME COLUMN analytics TO telemetry;`,
	`ALTER TABLE analytics_state RENAME TO telemetry_state;`,
}

const analyticsConsentSchema = `
ALTER TABLE settings ADD COLUMN analytics INTEGER CHECK (analytics IN (0, 1));

CREATE TABLE analytics_state (
  id                       INTEGER PRIMARY KEY CHECK (id = 1) REFERENCES settings(id),
  instance_id              TEXT NOT NULL,
  first_consent_date       TEXT,
  consent_start            TEXT,
  installation_acknowledged INTEGER NOT NULL DEFAULT 0 CHECK (installation_acknowledged IN (0, 1)),
  reported_through         TEXT,
  last_attempt             TEXT
) STRICT;

-- Generate a random UUID v4 once, inside the migration transaction. Copies of
-- the database intentionally retain the same identity.
INSERT INTO analytics_state (id, instance_id)
VALUES (1, lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' ||
  substr(hex(randomblob(2)), 2) || '-' ||
  substr('89ab', 1 + (random() & 3), 1) || substr(hex(randomblob(2)), 2) || '-' ||
  hex(randomblob(6))));
`

// initialSchema creates the complete database and seeds the built-in agents
// and instance settings. Ticket IDs are unique across projects.
const initialSchema = `
CREATE TABLE projects (
  id             INTEGER PRIMARY KEY,
  path           TEXT NOT NULL UNIQUE,
  default_branch TEXT NOT NULL,
  first_commit   TEXT
);

CREATE TABLE tickets (
  id             INTEGER PRIMARY KEY,
  project_id     INTEGER NOT NULL REFERENCES projects(id),
  title          TEXT NOT NULL,
  status         TEXT NOT NULL,
  position       INTEGER,
  branch         TEXT,
  session        TEXT,
  commit_id      TEXT,
  ready_position INTEGER,
  CHECK ((status = 'queued') = (position IS NOT NULL))
);

-- NULL positions let tickets outside each list coexist in these unique indexes.
CREATE UNIQUE INDEX tickets_position ON tickets(position);
CREATE UNIQUE INDEX tickets_ready_position ON tickets(ready_position);

CREATE TABLE queue_state (
  id      INTEGER PRIMARY KEY CHECK (id = 1),
  running BOOLEAN NOT NULL DEFAULT 1 CHECK (running IN (0, 1))
);
INSERT INTO queue_state (id, running) VALUES (1, 1);

-- JSON preserves argument boundaries in launch and resume commands.
CREATE TABLE agents (
  id           INTEGER PRIMARY KEY,
  name         TEXT NOT NULL UNIQUE,
  argv         TEXT NOT NULL DEFAULT '[]',
  resume_argv  TEXT NOT NULL DEFAULT '[]',
  install_hint TEXT
);

INSERT INTO agents (name, argv, resume_argv, install_hint) VALUES
  ('claude', '["claude-agent-acp"]', '["claude","--resume","{session}"]', 'Install with: npm install -g @agentclientprotocol/claude-agent-acp'),
  ('codex', '["codex-acp"]', '["codex","resume","{session}"]', 'Install with: npm install -g @agentclientprotocol/codex-acp'),
  ('gemini', '["gemini","--experimental-acp"]', '[]', 'Install Gemini CLI so "gemini --experimental-acp" is available on PATH'),
  ('opencode', '["opencode","acp"]', '["opencode","--session","{session}"]', 'Install OpenCode'),
  ('goose', '["goose","acp"]', '["goose","session","--resume","--session-id","{session}"]', 'Install Goose 1.50.1 or later'),
  ('github-copilot', '["copilot","--acp"]', '["copilot","--resume={session}"]', 'Install GitHub Copilot CLI 1.0.85 or later'),
  ('cursor', '["agent","acp"]', '[]', 'Install Cursor Agent 2026.09.15-d2fe57e or later'),
  ('pi', '["pi-acp"]', '["pi","--session","{session}"]', 'Install Pi 0.85.1 or later and pi-acp 0.0.33 or later');

CREATE TABLE runs (
  id         INTEGER PRIMARY KEY,
  ticket_id  INTEGER NOT NULL REFERENCES tickets(id),
  pid        INTEGER,
  started_at TEXT NOT NULL,
  ended_at   TEXT,
  exit_code  INTEGER,
  agent_id   INTEGER REFERENCES agents(id)
);

CREATE TABLE ticket_deps (
  ticket_id  INTEGER NOT NULL REFERENCES tickets(id),
  depends_on INTEGER NOT NULL REFERENCES tickets(id),
  PRIMARY KEY (ticket_id, depends_on),
  CHECK (ticket_id <> depends_on)
) WITHOUT ROWID;

CREATE INDEX ticket_deps_depends_on ON ticket_deps(depends_on);

-- Each state change is recorded once; arrival has no from_status.
CREATE TABLE transitions (
  id          INTEGER PRIMARY KEY,
  ticket_id   INTEGER NOT NULL REFERENCES tickets(id),
  from_status TEXT,
  to_status   TEXT NOT NULL,
  at          TEXT NOT NULL
);

CREATE INDEX transitions_ticket ON transitions(ticket_id, id);

CREATE TABLE settings (
  id                   INTEGER PRIMARY KEY CHECK (id = 1),
  runs                 INTEGER NOT NULL CHECK (runs >= 1),
  timeout_minutes      INTEGER NOT NULL CHECK (timeout_minutes >= 1),
  done_hours           INTEGER NOT NULL CHECK (done_hours >= 0),
  max_runs_per_project INTEGER NOT NULL CHECK (max_runs_per_project >= 0),
  default_agent_id     INTEGER REFERENCES agents(id)
) STRICT;

INSERT INTO settings
  (id, runs, timeout_minutes, done_hours, max_runs_per_project)
VALUES (1, 3, 60, 24, 0);

-- No row means no reported usage; NULL distinguishes an omitted count from zero.
CREATE TABLE run_usage (
  run_id              INTEGER PRIMARY KEY REFERENCES runs(id),
  input_tokens        INTEGER,
  cached_write_tokens INTEGER,
  cached_read_tokens  INTEGER,
  output_tokens       INTEGER,
  thought_tokens      INTEGER,
  total_tokens        INTEGER
) STRICT;
`

// migrate applies pending steps and updates PRAGMA user_version after each
// one. A current database needs no migrations.
func migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}

	// Refuse schemas newer than this binary understands.
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

// applyStep commits a migration and its version number atomically, rolling
// back both on failure.
func applyStep(db *sql.DB, i int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(migrations[i]); err != nil {
		return err
	}
	// PRAGMA does not accept parameters. The interpolated version comes
	// from our migration list, not user input.
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
		return err
	}
	return tx.Commit()
}
