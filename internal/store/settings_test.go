package store

import (
	"errors"
	"reflect"
	"testing"

	"github.com/alcubie/delegator/internal/config"
)

func TestSettingsMigrationSeedsDefaults(t *testing.T) {
	s, _ := emptyStore(t)
	got, err := s.Settings()
	if err != nil {
		t.Fatal(err)
	}
	want := config.Config{Runs: 2, TimeoutMinutes: 60, DoneHours: 24}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Settings = %+v, want defaults %+v", got, want)
	}
	var agentID any
	if err := s.db.QueryRow("SELECT default_agent_id FROM settings WHERE id = 1").Scan(&agentID); err != nil {
		t.Fatal(err)
	}
	if agentID != nil {
		t.Errorf("default_agent_id = %v, want NULL", agentID)
	}
}

func TestSettingsTableEnforcesItsConstraints(t *testing.T) {
	tests := []struct {
		name   string
		column string
		value  any
	}{
		{"runs", "runs", 0},
		{"runs type", "runs", "many"},
		{"timeout", "timeout_minutes", 0},
		{"done window", "done_hours", -1},
		{"project limit", "max_runs_per_project", -1},
		{"agent reference", "default_agent_id", -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := emptyStore(t)
			if _, err := s.db.Exec("UPDATE settings SET "+tt.column+" = ? WHERE id = 1", tt.value); err == nil {
				t.Fatalf("database accepted invalid %s", tt.column)
			}
		})
	}
}

func TestSettingUpdatesPersistAcrossOpen(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := config.Config{Runs: 3, TimeoutMinutes: 90, DoneHours: 72, MaxRunsPerProject: 2, DefaultAgent: "gemini"}
	for _, set := range []func() error{
		func() error { return s.SetSetting("runs", "3") },
		func() error { return s.SetSetting("timeout_minutes", "90") },
		func() error { return s.SetSetting("done_hours", "72") },
		func() error { return s.SetSetting("max_runs_per_project", "2") },
		func() error { return s.SetSetting("default_agent", "gemini") },
	} {
		if err := set(); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("reopened Settings = %+v, want %+v", got, want)
	}
}

func TestSettingUpdatesRejectInvalidValuesWithoutChangingSettings(t *testing.T) {
	tests := []struct {
		name string
		set  func(*Store) error
		want error
	}{
		{"runs", func(s *Store) error { return s.SetSetting("runs", "0") }, nil},
		{"timeout", func(s *Store) error { return s.SetSetting("timeout_minutes", "0") }, nil},
		{"done window", func(s *Store) error { return s.SetSetting("done_hours", "-1") }, nil},
		{"project limit", func(s *Store) error { return s.SetSetting("max_runs_per_project", "-1") }, nil},
		{"not an integer", func(s *Store) error { return s.SetSetting("runs", "many") }, nil},
		{"unknown setting", func(s *Store) error { return s.SetSetting("other", "1") }, nil},
		{"SQL in name", func(s *Store) error { return s.SetSetting("runs = 3 WHERE id = 1; --", "1") }, nil},
		{"empty agent", func(s *Store) error { return s.SetSetting("default_agent", "") }, ErrInvalidAgent},
		{"unknown agent", func(s *Store) error { return s.SetSetting("default_agent", "missing") }, ErrInvalidAgent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := emptyStore(t)
			before, err := s.Settings()
			if err != nil {
				t.Fatal(err)
			}
			err = tt.set(s)
			if err == nil {
				t.Fatal("setting update succeeded with an invalid value")
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("setting update error = %v, want %v", err, tt.want)
			}
			after, err := s.Settings()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after, before) {
				t.Errorf("settings changed to %+v, want %+v", after, before)
			}
		})
	}
}
