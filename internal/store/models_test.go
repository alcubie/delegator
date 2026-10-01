package store

import (
	"errors"
	"testing"
)

func TestRecordSessionRegistersModelsPerAgent(t *testing.T) {
	s, id := failedTicket(t)
	first, err := s.Run(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordSession(first.ID, "session-1", "model-v1"); err != nil {
		t.Fatal(err)
	}
	first, _ = s.Run(id)
	if !first.ModelID.Valid || first.Model != "model-v1" {
		t.Fatalf("model was not recorded: %+v", first)
	}
	for range 2 {
		runID, err := s.Restart(id, testAgentID)
		if err != nil {
			t.Fatal(err)
		}
		run, _ := s.Run(id)
		if run.ModelID != first.ModelID || run.Model != first.Model {
			t.Fatalf("restart lost model before setup: %+v", run)
		}
		if err := s.RecordSession(runID, "session-1", "model-v1"); err != nil {
			t.Fatal(err)
		}
		if err := s.ChangeStatus(id, Failed); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM models").Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate model registrations: %d, %v", count, err)
	}
	if err := s.SaveAgent(Agent{Name: "other", Argv: []string{"other"}}); err != nil {
		t.Fatal(err)
	}
	agent, _ := s.Agent("other")
	// With no saved session, a restart can select a different agent.
	if _, err := s.db.Exec("UPDATE tickets SET session = NULL WHERE id = ?", id); err != nil {
		t.Fatal(err)
	}
	runID, err := s.Restart(id, agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	run, _ := s.Run(id)
	if run.ModelID.Valid {
		t.Fatal("restart without a session inherited a model")
	}
	if err := s.RecordSession(runID, "session-2", "model-v1"); err != nil {
		t.Fatal(err)
	}
	run, _ = s.Run(id)
	if !run.ModelID.Valid || run.ModelID == first.ModelID || run.Model != first.Model {
		t.Fatalf("model identity was not scoped to agent: %+v", run)
	}
	if _, err := s.db.Exec("INSERT INTO models (agent_id, name) VALUES (?, ?)", agent.ID, "model-v1"); err == nil {
		t.Fatal("duplicate agent/model pair was accepted")
	}
}

func TestRecordSessionRollsBackModelWhenSessionWriteFails(t *testing.T) {
	s, id := failedTicket(t)
	run, _ := s.Run(id)
	if _, err := s.db.Exec(`CREATE TRIGGER reject_session BEFORE UPDATE OF session ON tickets
BEGIN SELECT RAISE(ABORT, 'session write failed'); END;`); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordSession(run.ID, "session", "model-v1"); err == nil {
		t.Fatal("session write unexpectedly succeeded")
	}
	after, _ := s.Run(id)
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM models").Scan(&count); err != nil || count != 0 || after.ModelID.Valid {
		t.Fatalf("failed setup left model state: count %d, run %+v, %v", count, after, err)
	}
	if err := s.RecordSession(-1, "session", "model-v1"); !errors.Is(err, ErrNoRun) {
		t.Fatalf("missing run error = %v", err)
	}
}

func TestRunModelMigrationLeavesExistingRunsUnknown(t *testing.T) {
	dir := t.TempDir()
	db := openRaw(t, dir)
	if _, err := db.Exec(initialSchema + telemetryConsentSchema + defaultModelSchema + `
INSERT INTO projects (id, path, default_branch) VALUES (1, '/repo', 'main');
INSERT INTO tickets (id, project_id, title, status, session) VALUES (1, 1, 'old ticket', 'failed', 'old-session');
INSERT INTO runs (id, ticket_id, agent_id, started_at) VALUES (1, 1, 1, '2026-01-01T00:00:00Z');
PRAGMA user_version = 3;`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	run, err := s.Run(1)
	if err != nil || run.ModelID.Valid || run.Model != "" || run.AgentID != 1 {
		t.Fatalf("migrated run = %+v, %v", run, err)
	}
	if err := s.RecordSession(1, "old-session", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Restart(1, 1); err != nil {
		t.Fatal(err)
	}
	run, err = s.Run(1)
	if err != nil || run.ModelID.Valid {
		t.Fatalf("unknown model restart = %+v, %v", run, err)
	}
}
