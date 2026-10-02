package analytics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/alcubie/delegator/internal/store"
)

const collectorURL = "https://telemetry.alcubi.ai/v1/reports"

type reportBatch struct {
	Format     int               `json:"format"`
	InstanceID string            `json:"instance_id"`
	Reports    []json.RawMessage `json:"reports"`
}

// Send attempts one due batch. Callers deliberately ignore its error so
// telemetry can never change command behavior.
func Send(s *store.Store, now time.Time, version string) error {
	client := &http.Client{
		Timeout:       5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return send(s, now, version, runtime.GOOS, collectorURL, client, MachineID)
}

func send(s *store.Store, now time.Time, version, goos, endpoint string, client *http.Client,
	machineID func(*bool) string) error {
	state, err := s.TelemetryState()
	if err != nil {
		return err
	}
	if state.Consent == nil || !*state.Consent || state.ConsentStart == nil || state.Revoked {
		return nil
	}
	metadata := Metadata{MachineID: machineID(state.Consent), Version: version, OS: goos}
	installation, err := BuildInstallation(state, metadata)
	if err != nil {
		return err
	}
	cutoff, daily, err := BuildDaily(s, now, metadata)
	if err != nil {
		return err
	}
	batch := reportBatch{Format: 1, InstanceID: state.InstanceID}
	if installation != nil {
		encoded, err := json.Marshal(installation)
		if err != nil {
			return err
		}
		batch.Reports = append(batch.Reports, encoded)
	}
	for _, report := range daily {
		encoded, err := json.Marshal(report)
		if err != nil {
			return err
		}
		batch.Reports = append(batch.Reports, encoded)
	}
	if len(batch.Reports) == 0 {
		return nil
	}
	reserved, err := s.ReserveTelemetryAttempt(now, *state.ConsentStart)
	if err != nil || !reserved {
		return err
	}
	current, err := s.TelemetryState()
	if err != nil {
		return err
	}
	if current.Consent == nil || !*current.Consent || current.ConsentStart == nil ||
		*current.ConsentStart != *state.ConsentStart || current.Revoked {
		return nil
	}
	body, err := json.Marshal(batch)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	httpClient := *client
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusGone {
		return s.RevokeTelemetry()
	}
	if response.StatusCode != http.StatusNoContent {
		if retryAt, ok := retryTime(response.Header.Get("Retry-After"), now); ok && retryAt.After(now.Add(15*time.Minute)) {
			if err := s.DeferTelemetryAttempt(retryAt); err != nil {
				return err
			}
		}
		return fmt.Errorf("telemetry collector returned %s", response.Status)
	}
	return s.AcknowledgeTelemetry(*state.ConsentStart, cutoff.Format(time.DateOnly), installation != nil)
}

func retryTime(value string, now time.Time) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= 0 {
		return now.Add(time.Duration(seconds) * time.Second), true
	}
	parsed, err := http.ParseTime(value)
	return parsed, err == nil
}
