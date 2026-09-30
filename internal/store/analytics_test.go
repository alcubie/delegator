package store

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func analyticsState(t *testing.T, s *Store) AnalyticsState {
	t.Helper()
	state, err := s.AnalyticsState()
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestAnalyticsMigrationPreservesSettingsAndSeedsUnansweredIdentity(t *testing.T) {
	dir := t.TempDir()
	db := openRaw(t, dir)
	if _, err := db.Exec(initialSchema + `
UPDATE settings SET runs = 7, default_agent_id = (SELECT id FROM agents WHERE name = 'codex');
PRAGMA user_version = 1;`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cfg, err := s.Settings()
	if err != nil || cfg.Telemetry != nil || cfg.Runs != 7 || cfg.DefaultAgent != "codex" {
		t.Fatalf("migrated settings = %+v, %v", cfg, err)
	}
	state := analyticsState(t, s)
	id, err := uuid.Parse(state.InstanceID)
	if err != nil || id.Version() != 4 || id.Variant() != uuid.RFC4122 {
		t.Fatalf("instance UUID = %q, %v", state.InstanceID, err)
	}
	if want := (AnalyticsState{InstanceID: state.InstanceID}); !reflect.DeepEqual(state, want) {
		t.Fatalf("unanswered state = %+v", state)
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := analyticsState(t, s); !reflect.DeepEqual(got, state) {
		t.Fatalf("reopen changed state: %+v", got)
	}
	other, _ := emptyStore(t)
	if got := analyticsState(t, other); got.InstanceID == state.InstanceID || got.Consent != nil {
		t.Fatalf("instances are not independent: %+v", got)
	}
}

func TestAnalyticsConsentTransitionsPreserveIdentityAndCooldown(t *testing.T) {
	s, _ := emptyStore(t)
	first := time.Date(2026, 9, 25, 17, 0, 0, 123, time.FixedZone("west", -7*3600))
	if err := s.setTelemetry(true, func() time.Time { return first }); err != nil {
		t.Fatal(err)
	}
	initial := analyticsState(t, s)
	if initial.Consent == nil || !*initial.Consent || *initial.FirstConsentDate != "2026-09-26" || *initial.ConsentStart != "2026-09-26T00:00:00.000000123Z" {
		t.Fatalf("first consent = %+v", initial)
	}
	if _, err := s.db.Exec(`UPDATE analytics_state SET installation_acknowledged = 1,
reported_through = '2026-09-28T00:00:00Z', last_attempt = '2026-09-28T12:00:00Z'`); err != nil {
		t.Fatal(err)
	}
	before := analyticsState(t, s)
	if err := s.SetSetting("telemetry", "true"); err != nil {
		t.Fatal(err)
	}
	if got := analyticsState(t, s); !reflect.DeepEqual(got, before) {
		t.Fatalf("repeated true changed state: %+v", got)
	}
	if err := s.SetSetting("telemetry", "false"); err != nil {
		t.Fatal(err)
	}
	disabled := analyticsState(t, s)
	if disabled.Consent == nil || *disabled.Consent {
		t.Fatal("disable did not persist false")
	}
	before.Consent = disabled.Consent
	if !reflect.DeepEqual(disabled, before) {
		t.Fatal("disable changed bookkeeping")
	}
	reenabled := first.Add(72 * time.Hour)
	if err := s.setTelemetry(true, func() time.Time { return reenabled }); err != nil {
		t.Fatal(err)
	}
	after := analyticsState(t, s)
	if after.Consent == nil || !*after.Consent || *after.ConsentStart != reenabled.UTC().Format(time.RFC3339Nano) || after.ReportedThrough != nil {
		t.Fatalf("new period not started: %+v", after)
	}
	if after.InstanceID != before.InstanceID || *after.FirstConsentDate != *before.FirstConsentDate || !after.InstallationAcknowledged || *after.LastAttempt != *before.LastAttempt {
		t.Fatalf("re-enable lost stable bookkeeping: %+v", after)
	}
}

func TestAnalyticsFalseAndInvalidValuesDoNotStartConsent(t *testing.T) {
	s, _ := emptyStore(t)
	for _, value := range []string{"null", "yes", "1", "", "TRUE"} {
		if err := s.SetSetting("telemetry", value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
	if err := s.SetSetting("telemetry", "false"); err != nil {
		t.Fatal(err)
	}
	state := analyticsState(t, s)
	if state.Consent == nil || *state.Consent || state.ConsentStart != nil || state.FirstConsentDate != nil {
		t.Fatalf("false started consent: %+v", state)
	}
	for _, value := range []any{2, -1, "yes"} {
		if _, err := s.db.Exec("UPDATE settings SET telemetry = ?", value); err == nil {
			t.Fatalf("database accepted telemetry %v", value)
		}
	}
}

func TestAnalyticsConsentAndBookkeepingAreAtomic(t *testing.T) {
	s, _ := emptyStore(t)
	if _, err := s.db.Exec(`CREATE TRIGGER reject_consent BEFORE UPDATE OF telemetry ON settings
BEGIN SELECT RAISE(ABORT, 'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	before := analyticsState(t, s)
	if err := s.SetSetting("telemetry", "true"); err == nil {
		t.Fatal("expected write failure")
	}
	if after := analyticsState(t, s); !reflect.DeepEqual(before, after) {
		t.Fatal("partial consent was committed")
	}
}

func TestConcurrentAnalyticsEnableKeepsOneConsentPeriod(t *testing.T) {
	dir := t.TempDir()
	first, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	var wg sync.WaitGroup
	for _, s := range []*Store{first, second} {
		wg.Go(func() {
			if err := s.SetSetting("telemetry", "true"); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	before := analyticsState(t, first)
	if err := second.SetSetting("telemetry", "true"); err != nil {
		t.Fatal(err)
	}
	if after := analyticsState(t, second); !reflect.DeepEqual(before, after) {
		t.Fatal("concurrent instances changed the consent period")
	}
}

func TestTelemetryRenamePreservesConsentAndReportingState(t *testing.T) {
	for _, consent := range []any{nil, false, true} {
		dir := t.TempDir()
		db := openRaw(t, dir)
		if _, err := db.Exec(initialSchema + analyticsConsentSchema + `PRAGMA user_version = 2;`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("UPDATE settings SET analytics = ?", consent); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE analytics_state SET
instance_id = '6fb76b4d-bb1f-461e-87fd-b6f16f69c4ed',
first_consent_date = '2026-09-25', consent_start = '2026-09-25T12:00:00.123Z',
installation_acknowledged = 1, reported_through = '2026-09-28T00:00:00Z',
last_attempt = '2026-09-28T12:00:00Z'`); err != nil {
			t.Fatal(err)
		}
		db.Close()
		s, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		cfg, err := s.Settings()
		if err != nil {
			t.Fatal(err)
		}
		var wantConsent *bool
		if value, ok := consent.(bool); ok {
			wantConsent = &value
		}
		if !reflect.DeepEqual(cfg.Telemetry, wantConsent) {
			t.Fatalf("renamed consent = %v, want %v", cfg.Telemetry, wantConsent)
		}
		date, start := "2026-09-25", "2026-09-25T12:00:00.123Z"
		cutoff, attempt := "2026-09-28T00:00:00Z", "2026-09-28T12:00:00Z"
		want := AnalyticsState{
			Consent: wantConsent, InstanceID: "6fb76b4d-bb1f-461e-87fd-b6f16f69c4ed",
			FirstConsentDate: &date, ConsentStart: &start, InstallationAcknowledged: true,
			ReportedThrough: &cutoff, LastAttempt: &attempt,
		}
		if got := analyticsState(t, s); !reflect.DeepEqual(got, want) {
			t.Fatalf("rename changed reporting state: %+v", got)
		}
		if err := s.SetSetting("analytics", "true"); err == nil {
			t.Fatal("old public setting name is still accepted")
		}
	}
}
