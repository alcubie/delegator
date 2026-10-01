package store

import (
	"database/sql"
	"time"
)

// TelemetryState is internal reporting bookkeeping, never a public config
// setting. Timestamps are UTC RFC3339Nano; FirstConsentDate is YYYY-MM-DD.
// ConsentStart preserves subsecond precision so partial days are never rounded
// back to midnight and rapid consent changes invalidate an older sender.
type TelemetryState struct {
	Consent                  *bool
	InstanceID               string
	FirstConsentDate         *string
	ConsentStart             *string
	InstallationAcknowledged bool
	ReportedThrough          *string
	LastAttempt              *string
}

// TelemetryState reads consent and bookkeeping together for future report
// builders and senders. Only Consent == true permits machine ID derivation.
func (s *Store) TelemetryState() (TelemetryState, error) {
	var state TelemetryState
	err := s.db.QueryRow(`SELECT settings.telemetry, state.instance_id,
state.first_consent_date, state.consent_start, state.installation_acknowledged,
state.reported_through, state.last_attempt
FROM telemetry_state AS state JOIN settings ON settings.id = state.id
WHERE state.id = 1`).Scan(&state.Consent, &state.InstanceID,
		&state.FirstConsentDate, &state.ConsentStart, &state.InstallationAcknowledged,
		&state.ReportedThrough, &state.LastAttempt)
	return state, err
}

func (s *Store) setTelemetry(enabled bool, clock func() time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var previous sql.NullBool
	if err := tx.QueryRow("SELECT telemetry FROM settings WHERE id = 1").Scan(&previous); err != nil {
		return err
	}
	if enabled && (!previous.Valid || !previous.Bool) {
		now := clock().UTC()
		// Retain first consent, installation acknowledgement and attempt cooldown
		// across opt-outs. A new period cannot backfill disabled or partial days.
		_, err = tx.Exec(`UPDATE telemetry_state SET
first_consent_date = COALESCE(first_consent_date, ?), consent_start = ?, reported_through = NULL
WHERE id = 1`, now.UTC().Format(time.DateOnly), now.UTC().Format(time.RFC3339Nano))
		if err != nil {
			return err
		}
	}
	if _, err := tx.Exec("UPDATE settings SET telemetry = ? WHERE id = 1", enabled); err != nil {
		return err
	}
	return tx.Commit()
}
