package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// RecordSession saves successful ACP setup before prompting. A model name is
// the exact value ID confirmed by the agent, including a reported default.
// Empty means the agent did not identify its model. Registry entries record
// selections, including explicit restart requests, not a guarantee of availability.
func (s *Store) RecordSession(runID int64, session, model string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var ticketID, agentID int64
	if err := tx.QueryRow("SELECT ticket_id, agent_id FROM runs WHERE id = ?", runID).Scan(&ticketID, &agentID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: run %d", ErrNoRun, runID)
		}
		return err
	}
	var modelID sql.Null[int64]
	if model != "" {
		modelID, err = registerModel(tx, agentID, model)
		if err != nil {
			return err
		}
	}
	if _, err := tx.Exec("UPDATE runs SET model_id = ? WHERE id = ?", modelID, runID); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE tickets SET session = ? WHERE id = ?", session, ticketID); err != nil {
		return err
	}
	return tx.Commit()
}

func registerModel(tx *sql.Tx, agentID int64, model string) (sql.Null[int64], error) {
	if _, err := tx.Exec("INSERT INTO models (agent_id, name) VALUES (?, ?) ON CONFLICT (agent_id, name) DO NOTHING", agentID, model); err != nil {
		return sql.Null[int64]{}, err
	}
	var modelID sql.Null[int64]
	err := tx.QueryRow("SELECT id FROM models WHERE agent_id = ? AND name = ?", agentID, model).Scan(&modelID)
	return modelID, err
}
