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

// migrations lists schema upgrades in order, starting at version 1. Never
// modify an applied step; append a new one so existing databases receive the
// change.
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
	verifyCursorACP,
	addRunUsageTable,
	verifyPiACP,
	addOnboardingInstallHints,
}

// tables creates projects, tickets, and the queue index. Ticket IDs are
// global across projects. The CHECK requires queued status and position
// together; the unique index permits multiple NULL positions for tickets
// outside the queue.
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

// completedColumn adds the legacy ready timestamp, originally used for DONE
// ordering and its display window. Later migrations replace it with
// transition history and acceptance time.
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

// addRunsTable stores process IDs, start and end times, and exit codes per
// run, allowing multiple runs per ticket. Active runs have NULL ended_at and
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

// addReadyPositionColumn adds user-controlled ordering for READY, separate
// from the queue position constrained by the original table. New ready
// tickets append to the list.
//
// Backfill preserves the old completion order, with IDs breaking ties and
// NULL completion times first.
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

// addDependenciesTable stores prerequisite edges: (5, 3) means ticket 5
// depends on ticket 3. The primary key prevents duplicates, the CHECK rejects
// self-dependencies, and the reverse index supports dependent lookups.
const addDependenciesTable = `
CREATE TABLE ticket_deps (
  ticket_id  INTEGER NOT NULL REFERENCES tickets(id),
  depends_on INTEGER NOT NULL REFERENCES tickets(id),
  PRIMARY KEY (ticket_id, depends_on),
  CHECK (ticket_id <> depends_on)
) WITHOUT ROWID;

CREATE INDEX ticket_deps_depends_on ON ticket_deps(depends_on);
`

// addTransitionsTable records status changes and their timestamps. The
// initial entry has no previous status. History replaces event-specific
// ticket columns that could become stale after a later transition.
//
// Backfill creation times for all tickets and ready times only for tickets
// still ready or done. Other tickets may have changed state at unknown times;
// recording their old completion as the latest transition would misrepresent
// their current status. Acceptance times cannot be recovered.
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

// dropCompletedColumn removes the ready timestamp now stored in transitions.
// A separate completion field became stale when a ticket left ready.
const dropCompletedColumn = `
ALTER TABLE tickets DROP COLUMN completed;
`

// dropCreatedColumn removes the creation timestamp after addTransitionsTable
// has copied it into each ticket's initial history entry.
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

// verifyCursorACP records the commands verified with Cursor Agent
// 2026.09.15-d2fe57e. Cursor serves ACP over stdio with "agent acp". Its
// terminal client does not resume an ACP session id, so there is no resume
// command to advertise.
const verifyCursorACP = `
UPDATE agents SET
  argv = '["agent","acp"]',
  resume_argv = '[]',
  install_hint = 'Install Cursor Agent 2026.09.15-d2fe57e or later'
WHERE name = 'cursor';
`

// addRunUsageTable keeps the aggregate token counts the ACP response reports
// for a prompt. There is one row at most for a run. Every count is nullable:
// no row distinguishes an agent that reports no usage from reported zero,
// and NULL keeps an optional category that was omitted distinct from zero.
const addRunUsageTable = `
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

// verifyPiACP records the commands verified with Pi 0.85.1 and pi-acp
// 0.0.33. The adapter serves ACP over stdio, while Pi resumes the underlying
// session by the id returned through ACP.
const verifyPiACP = `
UPDATE agents SET
  argv = '["pi-acp"]',
  resume_argv = '["pi","--session","{session}"]',
  install_hint = 'Install Pi 0.85.1 or later and pi-acp 0.0.33 or later'
WHERE name = 'pi';
`

// addOnboardingInstallHints fills the entries that predate registry hints, so
// dg init can give a useful next action even when no agent command is on PATH.
const addOnboardingInstallHints = `
UPDATE agents SET install_hint = 'Install with: npm install -g @agentclientprotocol/claude-agent-acp'
WHERE name = 'claude' AND COALESCE(install_hint, '') = '';

UPDATE agents SET install_hint = 'Install with: npm install -g @agentclientprotocol/codex-acp'
WHERE name = 'codex' AND COALESCE(install_hint, '') = '';

UPDATE agents SET install_hint = 'Install Gemini CLI so "gemini --experimental-acp" is available on PATH'
WHERE name = 'gemini' AND COALESCE(install_hint, '') = '';
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
