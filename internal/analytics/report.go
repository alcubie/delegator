package analytics

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/alcubie/delegator/internal/store"
	"github.com/google/uuid"
)

const ConsentNoticeVersion = 1

// Metadata is product information supplied by the caller. BuildDaily validates
// and normalizes it before it can enter a report.
type Metadata struct {
	MachineID string
	Version   string
	OS        string
}

type DailyReport struct {
	ID   string    `json:"id"`
	Kind string    `json:"kind"`
	Date string    `json:"date"`
	Data DailyData `json:"data"`
}

type InstallationReport struct {
	ID   string           `json:"id"`
	Kind string           `json:"kind"`
	Date string           `json:"date"`
	Data InstallationData `json:"data"`
}

type InstallationData struct {
	MachineID            *string `json:"machine_id,omitempty"`
	Version              string  `json:"version"`
	OS                   string  `json:"os"`
	ConsentNoticeVersion int     `json:"consent_notice_version"`
}

// BuildInstallation returns the stable installation report while it remains
// pending. The first consent date and instance identity survive re-enablement.
func BuildInstallation(state store.TelemetryState, metadata Metadata) (*InstallationReport, error) {
	if state.Consent == nil || !*state.Consent || state.InstallationAcknowledged || state.FirstConsentDate == nil {
		return nil, nil
	}
	namespace, err := uuid.Parse(state.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("parse telemetry instance ID: %w", err)
	}
	var machineID *string
	if hex64.MatchString(metadata.MachineID) {
		machineID = &metadata.MachineID
	}
	return &InstallationReport{
		ID: uuid.NewSHA1(namespace, []byte("installation")).String(), Kind: "installation", Date: *state.FirstConsentDate,
		Data: InstallationData{MachineID: machineID, Version: normalizedVersion(metadata.Version),
			OS: normalizedOS(metadata.OS), ConsentNoticeVersion: ConsentNoticeVersion},
	}, nil
}

type DailyData struct {
	MachineID               *string        `json:"machine_id,omitempty"`
	Version                 string         `json:"version"`
	OS                      string         `json:"os"`
	ConsentNoticeVersion    int            `json:"consent_notice_version"`
	TicketsCreated          int            `json:"tickets_created"`
	TicketsFirstReady       int            `json:"tickets_first_ready"`
	TicketsAccepted         int            `json:"tickets_accepted"`
	TicketsCancelled        int            `json:"tickets_cancelled"`
	RunsStarted             int            `json:"runs_started"`
	RunsEnded               int            `json:"runs_ended"`
	RunsEndedUnsuccessfully int            `json:"runs_ended_unsuccessfully"`
	RunsStartedByAgent      map[string]int `json:"runs_started_by_agent"`
	Settings                DailySettings  `json:"settings"`
}

type DailySettings struct {
	Runs              int     `json:"runs"`
	MaxRunsPerProject int     `json:"max_runs_per_project"`
	TimeoutMinutes    int     `json:"timeout_minutes"`
	DoneHours         int     `json:"done_hours"`
	DefaultAgent      *string `json:"default_agent"`
}

// BuildDaily captures the current UTC cutoff and builds every eligible whole
// day without changing consent, attempt, or cursor state.
func BuildDaily(s *store.Store, now time.Time, metadata Metadata) (time.Time, []DailyReport, error) {
	cutoff := now.UTC().Truncate(24 * time.Hour)
	floor := cutoff.AddDate(0, 0, -90)
	snapshot, err := s.TelemetrySnapshot(floor.Format(time.DateOnly), cutoff.Format(time.DateOnly))
	if err != nil {
		return cutoff, nil, err
	}
	if snapshot.State.Consent == nil || !*snapshot.State.Consent || snapshot.State.ConsentStart == nil {
		return cutoff, nil, nil
	}
	namespace, err := uuid.Parse(snapshot.State.InstanceID)
	if err != nil {
		return cutoff, nil, fmt.Errorf("parse telemetry instance ID: %w", err)
	}
	consent, err := time.Parse(time.RFC3339Nano, *snapshot.State.ConsentStart)
	if err != nil {
		return cutoff, nil, fmt.Errorf("parse telemetry consent start: %w", err)
	}
	start := consent.UTC().Truncate(24 * time.Hour)
	if !consent.UTC().Equal(start) {
		start = start.AddDate(0, 0, 1)
	}
	if snapshot.State.ReportedThrough != nil {
		reported, err := parseCutoff(*snapshot.State.ReportedThrough)
		if err != nil {
			return cutoff, nil, fmt.Errorf("parse telemetry cursor: %w", err)
		}
		if reported.After(start) {
			start = reported
		}
	}
	if floor.After(start) {
		start = floor
	}
	if !start.Before(cutoff) {
		return cutoff, nil, nil
	}

	activity := make(map[string]store.TelemetryDay, len(snapshot.Days))
	for _, day := range snapshot.Days {
		activity[day.Date] = day
	}
	settings := DailySettings{
		Runs: snapshot.Settings.Runs, MaxRunsPerProject: snapshot.Settings.MaxRunsPerProject,
		TimeoutMinutes: snapshot.Settings.TimeoutMinutes, DoneHours: snapshot.Settings.DoneHours,
		DefaultAgent: agentFamily(snapshot.Settings.DefaultAgent, snapshot.DefaultAgentArgv),
	}
	var machineID *string
	if hex64.MatchString(metadata.MachineID) {
		machineID = &metadata.MachineID
	}
	base := DailyData{MachineID: machineID, Version: normalizedVersion(metadata.Version),
		OS: normalizedOS(metadata.OS), ConsentNoticeVersion: ConsentNoticeVersion, Settings: settings}

	reports := make([]DailyReport, 0, int(cutoff.Sub(start)/(24*time.Hour)))
	for day := start; day.Before(cutoff); day = day.AddDate(0, 0, 1) {
		date := day.Format(time.DateOnly)
		a := activity[date]
		data := base
		data.TicketsCreated, data.TicketsFirstReady = a.TicketsCreated, a.TicketsFirstReady
		data.TicketsAccepted, data.TicketsCancelled = a.TicketsAccepted, a.TicketsCancelled
		data.RunsStarted, data.RunsEnded = a.RunsStarted, a.RunsEnded
		data.RunsEndedUnsuccessfully = a.RunsEndedUnsuccessfully
		data.RunsStartedByAgent = make(map[string]int)
		for _, count := range a.RunsStartedByAgent {
			family := agentFamily(count.Name, count.Argv)
			data.RunsStartedByAgent[*family] += count.Count
		}
		reports = append(reports, DailyReport{ID: uuid.NewSHA1(namespace, []byte("daily:"+date)).String(),
			Kind: "daily", Date: date, Data: data})
	}
	return cutoff, reports, nil
}

func parseCutoff(value string) (time.Time, error) {
	if t, err := time.Parse(time.DateOnly, value); err == nil {
		return t, nil
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	return t.UTC().Truncate(24 * time.Hour), err
}

var releaseVersion = regexp.MustCompile(`^v?([0-9]+\.[0-9]+\.[0-9]+)$`)
var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

func normalizedVersion(version string) string {
	match := releaseVersion.FindStringSubmatch(version)
	if match == nil {
		return "development"
	}
	return match[1]
}

func normalizedOS(name string) string {
	switch name {
	case "linux", "darwin", "windows", "freebsd":
		return name
	default:
		return "other"
	}
}

var builtInArgv = map[string]string{
	"claude": `["claude-agent-acp"]`, "codex": `["codex-acp"]`,
	"gemini": `["gemini","--experimental-acp"]`, "opencode": `["opencode","acp"]`,
	"goose": `["goose","acp"]`, "github-copilot": `["copilot","--acp"]`,
	"cursor": `["agent","acp"]`, "pi": `["pi-acp"]`,
}

func agentFamily(name, argv string) *string {
	if name == "" {
		return nil
	}
	family := "custom"
	if expected, ok := builtInArgv[name]; ok && strings.TrimSpace(argv) == expected {
		family = name
	}
	return &family
}
