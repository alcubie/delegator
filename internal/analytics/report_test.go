package analytics

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/store"
)

const testInstance = "11111111-1111-4111-8111-111111111111"

func reportStore(t *testing.T, consentStart string) (*store.Store, *sql.DB) {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(dir, "delegator.db")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`UPDATE settings SET telemetry = 1;
UPDATE telemetry_state SET instance_id = ?, first_consent_date = date(?), consent_start = ?,
reported_through = NULL, last_attempt = '2026-09-30T01:02:03Z'`, testInstance, consentStart, consentStart); err != nil {
		t.Fatal(err)
	}
	return s, db
}

func TestBuildDailyCountsWholeDaysAndExcludesSensitiveInput(t *testing.T) {
	s, db := reportStore(t, "2026-09-29T12:00:00Z")
	if err := s.SetDefaultAgent("goose"); err != nil {
		t.Fatal(err)
	}
	_, err := db.Exec(`
INSERT INTO projects (id, path, default_branch) VALUES (1, '/secret/customer/project', 'private-branch');
INSERT INTO tickets (id, project_id, title, status) VALUES
 (1, 1, 'secret ticket one', 'done'), (2, 1, 'secret ticket two', 'cancelled');
INSERT INTO transitions (ticket_id, from_status, to_status, at) VALUES
 (1, NULL, 'queued', '2026-09-29T23:59:59Z'),
 (2, NULL, 'queued', '2026-09-30T00:00:00Z'),
 (1, 'running', 'ready', '2026-10-01T00:00:00Z'),
 (1, 'failed', 'ready', '2026-10-02T01:00:00Z'),
 (1, 'ready', 'done', '2026-10-02T02:00:00Z'),
 (2, 'ready', 'cancelled', '2026-10-02T03:00:00Z');
INSERT INTO agents (id, name, argv, resume_argv, install_hint)
 VALUES (99, 'secret-agent-name', '["/secret/bin","--token=secret"]', '[]', 'secret install');
UPDATE agents SET argv = '["codex-acp","--secret-option"]' WHERE name = 'codex';
INSERT INTO runs (ticket_id, agent_id, started_at, ended_at, exit_code) VALUES
 (1, 2, '2026-09-30T23:59:59Z', '2026-10-01T00:00:01Z', 0),
 (1, 99, '2026-10-01T12:00:00Z', '2026-10-02T00:00:00Z', NULL),
 (2, 5, '2026-10-01T13:00:00Z', '2026-10-02T02:00:00Z', 7);`)
	if err != nil {
		t.Fatal(err)
	}

	before, err := s.TelemetryState()
	if err != nil {
		t.Fatal(err)
	}
	cutoff, reports, err := BuildDaily(s, time.Date(2026, 10, 4, 18, 0, 0, 0, time.FixedZone("local", -7*3600)),
		Metadata{MachineID: strings.Repeat("a", 64), Version: "v1.2.3", OS: "linux"})
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC); !cutoff.Equal(want) {
		t.Fatalf("cutoff = %s, want %s", cutoff, want)
	}
	if len(reports) != 5 || reports[0].Date != "2026-09-30" || reports[4].Date != "2026-10-04" {
		t.Fatalf("report dates = %#v", reportDates(reports))
	}
	sep30, oct1, oct2, oct3 := reports[0].Data, reports[1].Data, reports[2].Data, reports[3].Data
	if sep30.TicketsCreated != 1 || sep30.RunsStarted != 1 || sep30.RunsEnded != 0 || sep30.RunsStartedByAgent["custom"] != 1 {
		t.Errorf("September 30 = %+v", sep30)
	}
	if oct1.TicketsFirstReady != 1 || oct1.RunsStarted != 2 || oct1.RunsEnded != 1 ||
		oct1.RunsStartedByAgent["goose"] != 1 || oct1.RunsStartedByAgent["custom"] != 1 {
		t.Errorf("October 1 = %+v", oct1)
	}
	if oct2.TicketsFirstReady != 0 || oct2.TicketsAccepted != 1 || oct2.TicketsCancelled != 1 ||
		oct2.RunsEnded != 2 || oct2.RunsEndedUnsuccessfully != 2 {
		t.Errorf("October 2 = %+v", oct2)
	}
	if oct3.TicketsCreated+oct3.RunsStarted+oct3.RunsEnded != 0 || len(oct3.RunsStartedByAgent) != 0 {
		t.Errorf("idle October 3 = %+v", oct3)
	}
	after, err := s.TelemetryState()
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatalf("builder changed telemetry state: %+v -> %+v (%v)", before, after, err)
	}

	raw, err := json.Marshal(reports)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"secret", "customer", "private-branch", "token"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("serialized reports contain %q: %s", secret, raw)
		}
	}

	firstIDs := reportIDs(reports)
	if _, err := db.Exec(`UPDATE settings SET runs = 8, timeout_minutes = 99`); err != nil {
		t.Fatal(err)
	}
	_, changed, err := BuildDaily(s, cutoff.Add(25*time.Hour), Metadata{Version: "v1.2.3-2-gsecret", OS: "private-os"})
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != len(reports)+1 || !reflect.DeepEqual(reportIDs(changed[:len(reports)]), firstIDs) {
		t.Errorf("IDs changed with metadata/settings: %v -> %v", firstIDs, reportIDs(changed))
	}
	if changed[0].Data.Version != "development" || changed[0].Data.OS != "other" || changed[0].Data.MachineID != nil {
		t.Errorf("unsafe metadata was not normalized: %+v", changed[0].Data)
	}
	if _, err := db.Exec(`UPDATE telemetry_state SET consent_start = '2026-10-02T00:00:00Z', reported_through = NULL`); err != nil {
		t.Fatal(err)
	}
	_, reenabled, err := BuildDaily(s, cutoff.Add(25*time.Hour), Metadata{})
	if err != nil {
		t.Fatal(err)
	}
	if reenabled[0].Date != "2026-10-02" || reenabled[0].ID != reports[2].ID {
		t.Errorf("ID changed across consent periods: %s -> %s", reports[2].ID, reenabled[0].ID)
	}
}

func TestBuildDailyConsentCursorAndRetentionBoundaries(t *testing.T) {
	tests := []struct {
		name, consent, cursor string
		now                   time.Time
		enabled               bool
		wantFirst             string
		wantCount             int
		wantID                string
	}{
		{"contract UUID", "2026-09-25T12:00:00Z", "", time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC), true, "2026-09-26", 1, "b5fc6b96-2d2d-5d0a-aafa-9942bbdca2e3"},
		{"midday reenable", "2026-10-02T12:00:00Z", "", time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC), true, "2026-10-03", 1, ""},
		{"midnight consent", "2026-10-02T00:00:00Z", "", time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC), true, "2026-10-02", 2, ""},
		{"acknowledged cutoff", "2026-09-01T00:00:00Z", "2026-10-03T00:00:00Z", time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC), true, "2026-10-03", 1, ""},
		{"ninety day expiry", "2026-01-01T00:00:00Z", "", time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC), true, "2026-07-06", 90, ""},
		{"disabled", "2026-10-02T00:00:00Z", "", time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC), false, "", 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, db := reportStore(t, tt.consent)
			if !tt.enabled {
				if _, err := db.Exec(`UPDATE settings SET telemetry = 0`); err != nil {
					t.Fatal(err)
				}
			}
			if tt.cursor != "" {
				if _, err := db.Exec(`UPDATE telemetry_state SET reported_through = ?`, tt.cursor); err != nil {
					t.Fatal(err)
				}
			}
			_, reports, err := BuildDaily(s, tt.now, Metadata{})
			if err != nil {
				t.Fatal(err)
			}
			if len(reports) != tt.wantCount {
				t.Fatalf("got %d reports, want %d", len(reports), tt.wantCount)
			}
			if len(reports) > 0 && reports[0].Date != tt.wantFirst {
				t.Errorf("first date = %s, want %s", reports[0].Date, tt.wantFirst)
			}
			if tt.wantID != "" && reports[0].ID != tt.wantID {
				t.Errorf("report ID = %s, want accepted contract ID %s", reports[0].ID, tt.wantID)
			}
		})
	}
}

func reportDates(reports []DailyReport) []string {
	out := make([]string, len(reports))
	for i := range reports {
		out[i] = reports[i].Date
	}
	return out
}

func reportIDs(reports []DailyReport) []string {
	out := make([]string, len(reports))
	for i := range reports {
		out[i] = reports[i].ID
	}
	return out
}
