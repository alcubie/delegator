package config

import (
	"testing"
	"time"
)

func TestDoneWindowIsTheHoursOfTheSetting(t *testing.T) {
	if got := (Config{DoneHours: 3}).DoneWindow(); got != 3*time.Hour {
		t.Errorf("DoneWindow = %v, want %v", got, 3*time.Hour)
	}
	if got := (Config{DoneHours: 0}).DoneWindow(); got != 0 {
		t.Errorf("DoneWindow = %v, want 0", got)
	}
}

func TestTimeoutIsTheMinutesOfTheSetting(t *testing.T) {
	if got := (Config{TimeoutMinutes: 90}).Timeout(); got != 90*time.Minute {
		t.Errorf("Timeout = %v, want %v", got, 90*time.Minute)
	}
	if got := (Config{TimeoutMinutes: 0}).Timeout(); got != 0 {
		t.Errorf("Timeout = %v, want 0", got)
	}
}

func TestProjectRunsIsTheSettingAndFallsBackToRuns(t *testing.T) {
	for _, tc := range []struct {
		cfg  Config
		want int
	}{
		{cfg: Config{Runs: 3, MaxRunsPerProject: 1}, want: 1},
		{cfg: Config{Runs: 3, MaxRunsPerProject: 5}, want: 5},
		{cfg: Config{Runs: 3, MaxRunsPerProject: 0}, want: 3},
		{cfg: Config{Runs: 3, MaxRunsPerProject: -1}, want: 3},
	} {
		if got := tc.cfg.ProjectRuns(); got != tc.want {
			t.Errorf("ProjectRuns of %+v = %d, want %d", tc.cfg, got, tc.want)
		}
	}
}
