package store

import (
	"database/sql"
	"time"

	"github.com/alcubie/delegator/internal/config"
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
	Revoked                  bool
}

// TelemetrySnapshot is the read-only input to the telemetry report builder.
// Agent registry details stay local and are used only to recognize unmodified
// built-in agents; report payloads never include them.
type TelemetrySnapshot struct {
	State            TelemetryState
	Settings         config.Config
	DefaultAgentArgv string
	Days             []TelemetryDay
}

type TelemetryDay struct {
	Date                    string
	TicketsCreated          int
	TicketsFirstReady       int
	TicketsAccepted         int
	TicketsCancelled        int
	RunsStarted             int
	RunsEnded               int
	RunsEndedUnsuccessfully int
	RunsStartedByAgent      []TelemetryAgentCount
}

type TelemetryAgentCount struct {
	Name  string
	Argv  string
	Count int
}

// TelemetryState reads consent and bookkeeping together for future report
// builders and senders. Only Consent == true permits machine ID derivation.
func (s *Store) TelemetryState() (TelemetryState, error) {
	var state TelemetryState
	err := s.db.QueryRow(`SELECT settings.telemetry, state.instance_id,
state.first_consent_date, state.consent_start, state.installation_acknowledged,
state.reported_through, state.last_attempt, state.revoked
FROM telemetry_state AS state JOIN settings ON settings.id = state.id
WHERE state.id = 1`).Scan(&state.Consent, &state.InstanceID,
		&state.FirstConsentDate, &state.ConsentStart, &state.InstallationAcknowledged,
		&state.ReportedThrough, &state.LastAttempt, &state.Revoked)
	return state, err
}

// TelemetrySnapshot reads reporting state, current settings, and committed
// history in one transaction. start and end are UTC dates with end exclusive.
func (s *Store) TelemetrySnapshot(start, end string) (TelemetrySnapshot, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return TelemetrySnapshot{}, err
	}
	defer tx.Rollback()

	var out TelemetrySnapshot
	err = tx.QueryRow(`SELECT settings.telemetry, state.instance_id,
state.first_consent_date, state.consent_start, state.installation_acknowledged,
state.reported_through, state.last_attempt, state.revoked, settings.runs, settings.timeout_minutes,
settings.done_hours, settings.max_runs_per_project, COALESCE(agents.name, ''),
COALESCE(agents.argv, ''), COALESCE(settings.default_model, '')
FROM telemetry_state AS state JOIN settings ON settings.id = state.id
LEFT JOIN agents ON agents.id = settings.default_agent_id WHERE state.id = 1`).Scan(
		&out.State.Consent, &out.State.InstanceID, &out.State.FirstConsentDate,
		&out.State.ConsentStart, &out.State.InstallationAcknowledged,
		&out.State.ReportedThrough, &out.State.LastAttempt, &out.State.Revoked, &out.Settings.Runs,
		&out.Settings.TimeoutMinutes, &out.Settings.DoneHours,
		&out.Settings.MaxRunsPerProject, &out.Settings.DefaultAgent,
		&out.DefaultAgentArgv, &out.Settings.DefaultModel)
	if err != nil {
		return TelemetrySnapshot{}, err
	}
	out.Settings.Telemetry = out.State.Consent
	if out.State.Consent == nil || !*out.State.Consent {
		return out, tx.Commit()
	}

	rows, err := tx.Query(`WITH first_ready AS (
	SELECT ticket_id, MIN(at) AS at FROM transitions WHERE to_status = 'ready' GROUP BY ticket_id
), activity AS (
	SELECT date(at) day, 'created' metric, '' agent, '' argv, COUNT(DISTINCT ticket_id) n
	  FROM transitions WHERE from_status IS NULL GROUP BY day
	UNION ALL SELECT date(at), 'ready', '', '', COUNT(*) FROM first_ready GROUP BY date(at)
	UNION ALL SELECT date(at), 'accepted', '', '', COUNT(*) FROM transitions WHERE to_status = 'done' GROUP BY date(at)
	UNION ALL SELECT date(at), 'cancelled', '', '', COUNT(*) FROM transitions WHERE to_status = 'cancelled' GROUP BY date(at)
	UNION ALL SELECT date(started_at), 'run_started', COALESCE(agents.name, ''), COALESCE(agents.argv, ''), COUNT(*)
	  FROM runs LEFT JOIN agents ON agents.id = runs.agent_id GROUP BY date(started_at), agents.name, agents.argv
	UNION ALL SELECT date(ended_at), 'run_ended', '', '', COUNT(*) FROM runs WHERE ended_at IS NOT NULL GROUP BY date(ended_at)
	UNION ALL SELECT date(ended_at), 'run_failed', '', '', COUNT(*) FROM runs
	  WHERE ended_at IS NOT NULL AND (exit_code IS NULL OR exit_code != 0) GROUP BY date(ended_at)
)
SELECT day, metric, agent, argv, n FROM activity WHERE day >= ? AND day < ? ORDER BY day`, start, end)
	if err != nil {
		return TelemetrySnapshot{}, err
	}
	defer rows.Close()
	byDate := make(map[string]*TelemetryDay)
	for rows.Next() {
		var day, metric, agent, argv string
		var count int
		if err := rows.Scan(&day, &metric, &agent, &argv, &count); err != nil {
			return TelemetrySnapshot{}, err
		}
		d := byDate[day]
		if d == nil {
			d = &TelemetryDay{Date: day}
			byDate[day] = d
		}
		switch metric {
		case "created":
			d.TicketsCreated = count
		case "ready":
			d.TicketsFirstReady = count
		case "accepted":
			d.TicketsAccepted = count
		case "cancelled":
			d.TicketsCancelled = count
		case "run_started":
			d.RunsStarted += count
			d.RunsStartedByAgent = append(d.RunsStartedByAgent, TelemetryAgentCount{Name: agent, Argv: argv, Count: count})
		case "run_ended":
			d.RunsEnded = count
		case "run_failed":
			d.RunsEndedUnsuccessfully = count
		}
	}
	if err := rows.Err(); err != nil {
		return TelemetrySnapshot{}, err
	}
	for day := start; day < end; {
		if d := byDate[day]; d != nil {
			out.Days = append(out.Days, *d)
		}
		t, err := time.Parse(time.DateOnly, day)
		if err != nil {
			return TelemetrySnapshot{}, err
		}
		day = t.AddDate(0, 0, 1).Format(time.DateOnly)
	}
	return out, tx.Commit()
}

// ReserveTelemetryAttempt prevents concurrent processes and rapid retries from
// sending the same batch. expectedConsent identifies the period used to build
// the payload; a disable/re-enable race makes the reservation fail.
func (s *Store) ReserveTelemetryAttempt(now time.Time, expectedConsent string) (bool, error) {
	threshold := now.UTC().Add(-15 * time.Minute).Format(time.RFC3339Nano)
	result, err := s.db.Exec(`UPDATE telemetry_state SET last_attempt = ?
WHERE id = 1 AND revoked = 0 AND consent_start = ?
  AND EXISTS (SELECT 1 FROM settings WHERE id = 1 AND telemetry = 1)
  AND (last_attempt IS NULL OR julianday(last_attempt) <= julianday(?))`,
		now.UTC().Format(time.RFC3339Nano), expectedConsent, threshold)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	return changed == 1, err
}

// DeferTelemetryAttempt honors a server delay longer than the ordinary
// cooldown. Storing retryAt-minus-cooldown preserves the one timestamp model.
func (s *Store) DeferTelemetryAttempt(retryAt time.Time) error {
	value := retryAt.UTC().Add(-15 * time.Minute).Format(time.RFC3339Nano)
	_, err := s.db.Exec(`UPDATE telemetry_state SET last_attempt = ?
WHERE id = 1 AND (last_attempt IS NULL OR julianday(last_attempt) < julianday(?))`, value, value)
	return err
}

// AcknowledgeTelemetry records a whole successful request only while its
// original consent period is still active.
func (s *Store) AcknowledgeTelemetry(consentStart, cutoff string, installation bool) error {
	_, err := s.db.Exec(`UPDATE telemetry_state SET
installation_acknowledged = CASE WHEN ? THEN 1 ELSE installation_acknowledged END,
reported_through = ?
WHERE id = 1 AND consent_start = ?
  AND EXISTS (SELECT 1 FROM settings WHERE id = 1 AND telemetry = 1)`,
		installation, cutoff, consentStart)
	return err
}

// RevokeTelemetry permanently suppresses this instance after the collector
// rejects its stable identity. Re-enabling consent does not rotate that ID.
func (s *Store) RevokeTelemetry() error {
	_, err := s.db.Exec(`UPDATE telemetry_state SET revoked = 1 WHERE id = 1`)
	return err
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
