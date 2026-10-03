package analytics

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/store"
)

func localSend(s *store.Store, now time.Time, server *httptest.Server) error {
	return send(s, now, "v1.2.3", "linux", server.URL, server.Client(),
		func(*bool) string { return "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" })
}

func decodeBatch(t *testing.T, request *http.Request) reportBatch {
	t.Helper()
	var batch reportBatch
	if err := json.NewDecoder(request.Body).Decode(&batch); err != nil {
		t.Fatal(err)
	}
	return batch
}

func reportKindsAndDates(t *testing.T, batch reportBatch) []string {
	t.Helper()
	var out []string
	for _, raw := range batch.Reports {
		var report struct{ Kind, Date string }
		if err := json.Unmarshal(raw, &report); err != nil {
			t.Fatal(err)
		}
		out = append(out, report.Kind+":"+report.Date)
	}
	return out
}

func TestSendRequiresConsentAndAcknowledgesInstallation(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		batch := decodeBatch(t, r)
		if batch.Format != 1 || batch.InstanceID != testInstance ||
			!slices.Equal(reportKindsAndDates(t, batch), []string{"installation:2026-09-25"}) {
			t.Errorf("batch = %+v, reports = %v", batch, reportKindsAndDates(t, batch))
		}
		var installation InstallationReport
		if err := json.Unmarshal(batch.Reports[0], &installation); err != nil {
			t.Error(err)
		} else if installation.ID != "3d5e1cef-16d1-5164-9aa0-7eadb18b647a" ||
			installation.Data.Version != "1.2.3" || installation.Data.MachineID == nil {
			t.Errorf("installation = %+v", installation)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	unanswered, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer unanswered.Close()
	now := time.Date(2026, 9, 25, 12, 1, 0, 0, time.UTC)
	if err := localSend(unanswered, now, server); err != nil {
		t.Fatal(err)
	}
	s, db := reportStore(t, "2026-09-25T12:00:00Z")
	if _, err := db.Exec(`UPDATE telemetry_state SET last_attempt = NULL`); err != nil {
		t.Fatal(err)
	}
	if err := localSend(s, now, server); err != nil {
		t.Fatal(err)
	}
	state, err := s.TelemetryState()
	if err != nil || !state.InstallationAcknowledged || *state.ReportedThrough != "2026-09-25" {
		t.Fatalf("acknowledged state = %+v, %v", state, err)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1", requests.Load())
	}
}

func TestSendLostReplyCooldownAndMidnightExpandedRetry(t *testing.T) {
	s, db := reportStore(t, "2026-09-25T12:00:00Z")
	if _, err := db.Exec(`UPDATE telemetry_state SET last_attempt = NULL`); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var batches []reportBatch
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		batch := decodeBatch(t, r)
		mu.Lock()
		batches = append(batches, batch)
		attempt := len(batches)
		mu.Unlock()
		if attempt == 1 {
			hijacker := w.(http.Hijacker)
			connection, _, err := hijacker.Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			connection.Close()
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	first := time.Date(2026, 9, 29, 23, 50, 0, 0, time.UTC)
	if err := localSend(s, first, server); err == nil {
		t.Fatal("lost reply unexpectedly succeeded")
	}
	if err := localSend(s, first.Add(14*time.Minute), server); err != nil {
		t.Fatal(err)
	}
	if err := localSend(s, first.Add(16*time.Minute), server); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(batches) != 2 {
		t.Fatalf("requests = %d, want 2", len(batches))
	}
	firstReports := reportKindsAndDates(t, batches[0])
	secondReports := reportKindsAndDates(t, batches[1])
	if !slices.Equal(firstReports, []string{"installation:2026-09-25", "daily:2026-09-26", "daily:2026-09-27", "daily:2026-09-28"}) {
		t.Errorf("first reports = %v", firstReports)
	}
	if !slices.Equal(secondReports[:len(firstReports)], firstReports) || secondReports[len(secondReports)-1] != "daily:2026-09-29" {
		t.Errorf("expanded retry = %v", secondReports)
	}
	state, err := s.TelemetryState()
	if err != nil || state.ReportedThrough == nil || *state.ReportedThrough != "2026-09-30" {
		t.Errorf("captured cutoff state = %+v, %v", state, err)
	}
}

func TestSendConsentRaceAndConcurrentReservation(t *testing.T) {
	s, db := reportStore(t, "2026-09-25T12:00:00Z")
	if _, err := db.Exec(`UPDATE telemetry_state SET last_attempt = NULL`); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 26, 1, 0, 0, 0, time.UTC)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if err := s.SetSetting("telemetry", "false"); err != nil {
			t.Error(err)
		}
		if err := s.SetSetting("telemetry", "true"); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { _ = localSend(s, now, server) })
	}
	wg.Wait()
	state, err := s.TelemetryState()
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 || state.Consent == nil || !*state.Consent ||
		state.InstallationAcknowledged || state.ReportedThrough != nil {
		t.Fatalf("requests = %d, state = %+v", requests.Load(), state)
	}
}

func TestSendHonorsRetryAfterAndRevocation(t *testing.T) {
	s, db := reportStore(t, "2026-09-25T12:00:00Z")
	if _, err := db.Exec(`UPDATE telemetry_state SET last_attempt = NULL`); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 25, 13, 0, 0, 0, time.UTC)
	var status atomic.Int32
	status.Store(http.StatusTooManyRequests)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if status.Load() == http.StatusTooManyRequests {
			w.Header().Set("Retry-After", "3600")
		}
		w.WriteHeader(int(status.Load()))
	}))
	defer server.Close()

	if err := localSend(s, now, server); err == nil {
		t.Fatal("429 unexpectedly succeeded")
	}
	status.Store(http.StatusGone)
	if err := localSend(s, now.Add(59*time.Minute), server); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("server retry delay sent %d requests", requests.Load())
	}
	if err := localSend(s, now.Add(time.Hour), server); err != nil {
		t.Fatal(err)
	}
	if err := localSend(s, now.Add(2*time.Hour), server); err != nil {
		t.Fatal(err)
	}
	state, err := s.TelemetryState()
	if err != nil || !state.Revoked || requests.Load() != 2 {
		t.Fatalf("revocation state = %+v, requests = %d, err = %v", state, requests.Load(), err)
	}
}

func TestSendDoesNotFollowRedirects(t *testing.T) {
	s, db := reportStore(t, "2026-09-25T12:00:00Z")
	if _, err := db.Exec(`UPDATE telemetry_state SET last_attempt = NULL`); err != nil {
		t.Fatal(err)
	}
	var redirected atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirected.Add(1)
	}))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()

	now := time.Date(2026, 9, 25, 13, 0, 0, 0, time.UTC)
	if err := localSend(s, now, server); err == nil {
		t.Fatal("redirect unexpectedly succeeded")
	}
	if redirected.Load() != 0 {
		t.Fatal("sender followed the collector redirect")
	}
}
