package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

// writeConfig writes text to config.toml below dir/delegator, so the test
// says where a file is once and each test reads it from the same place.
func writeConfig(t *testing.T, dir, text string) {
	t.Helper()
	path := filepath.Join(dir, "delegator", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadReadsTheFileBelowXDGConfigHome(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	writeConfig(t, xdg, "runs = 3\n")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Runs != 3 {
		t.Errorf("Runs = %d, want 3", cfg.Runs)
	}
}

func TestLoadReadsTheFileBelowHomeWithNoXDGConfigHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	writeConfig(t, filepath.Join(home, ".config"), "runs = 3\n")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Runs != 3 {
		t.Errorf("Runs = %d, want 3", cfg.Runs)
	}
}

func TestLoadWithNoFileGivesTheDefaultsAndNoError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load with no file: %v", err)
	}
	if cfg.Runs != 1 {
		t.Errorf("Runs = %d, want the default 1", cfg.Runs)
	}
}

func TestLoadGivesRunsTheDefaultWhenTheFileDoesNotSetIt(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	writeConfig(t, xdg, "timeout_minutes = 5\n")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Runs != 1 {
		t.Errorf("Runs = %d, want the default 1", cfg.Runs)
	}
}

func TestLoadReadsTimeoutMinutes(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	writeConfig(t, xdg, "timeout_minutes = 5\n")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TimeoutMinutes != 5 {
		t.Errorf("TimeoutMinutes = %d, want 5", cfg.TimeoutMinutes)
	}
}

func TestLoadGivesTimeoutMinutesTheDefaultWhenTheFileDoesNotSetIt(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	writeConfig(t, xdg, "runs = 3\n")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TimeoutMinutes != 60 {
		t.Errorf("TimeoutMinutes = %d, want the default 60", cfg.TimeoutMinutes)
	}
}

func TestLoadRefusesAValueOfTheWrongTypeAndNamesTheKey(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	writeConfig(t, xdg, "runs = \"three\"\n")

	_, err := Load()
	if err == nil {
		t.Fatal("Load accepted runs = \"three\"")
	}
	if !strings.Contains(err.Error(), "runs") {
		t.Errorf("the error does not name the key runs: %v", err)
	}
}

func TestLoadRefusesAKeyItDoesNotKnowAndNamesIt(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	writeConfig(t, xdg, "run = 3\n")

	_, err := Load()
	if err == nil {
		t.Fatal("Load accepted the key run, which it does not know")
	}
	if !strings.Contains(err.Error(), "run") {
		t.Errorf("the error does not name the key run: %v", err)
	}
}

func TestLoadReadsDoneHours(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	writeConfig(t, xdg, "done_hours = 72\n")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DoneHours != 72 {
		t.Errorf("DoneHours = %d, want 72", cfg.DoneHours)
	}
}

// The window of DONE is one day for a person who has set no value, so a
// ticket accepted this morning is still in the inbox this evening.
func TestLoadGivesDoneHoursTheDefaultOfOneDayWhenTheFileDoesNotSetIt(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	writeConfig(t, xdg, "runs = 3\n")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DoneHours != 24 {
		t.Errorf("DoneHours = %d, want the default 24", cfg.DoneHours)
	}
}

// The key is in hours, and DoneWindow is the one place that says so.
func TestDoneWindowIsTheHoursOfTheKey(t *testing.T) {
	if got := (Config{DoneHours: 3}).DoneWindow(); got != 3*time.Hour {
		t.Errorf("DoneWindow = %v, want %v", got, 3*time.Hour)
	}
	if got := (Config{DoneHours: 0}).DoneWindow(); got != 0 {
		t.Errorf("DoneWindow = %v, want 0", got)
	}
}

// The key is in minutes, and Timeout is the one place that says so. A value of
// 0 is no limit on a run, and not a limit of no time.
func TestTimeoutIsTheMinutesOfTheKey(t *testing.T) {
	if got := (Config{TimeoutMinutes: 90}).Timeout(); got != 90*time.Minute {
		t.Errorf("Timeout = %v, want %v", got, 90*time.Minute)
	}
	if got := (Config{TimeoutMinutes: 0}).Timeout(); got != 0 {
		t.Errorf("Timeout = %v, want 0", got)
	}
}

// Init writes the embedded file, and Load refuses a key it does not know, so
// a key in default.toml that the struct has no field for would make the file
// a person gets on first run the one file delegator cannot read.
func TestTheEmbeddedFileHoldsNoKeyTheStructDoesNotKnow(t *testing.T) {
	var cfg Config
	md, err := toml.Decode(string(defaultFile), &cfg)
	if err != nil {
		t.Fatal(err)
	}
	if unknown := md.Undecoded(); len(unknown) > 0 {
		t.Errorf("default.toml holds the key %q, which Config does not know", unknown[0].String())
	}
}

func TestLoadReadsMaxRunsPerProject(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	writeConfig(t, xdg, "max_runs_per_project = 2\n")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxRunsPerProject != 2 {
		t.Errorf("MaxRunsPerProject = %d, want 2", cfg.MaxRunsPerProject)
	}
}

// A person who has the file they had before this key gets the queue they had.
// The default is no limit for each project, so every project takes runs.
func TestLoadGivesMaxRunsPerProjectNoLimitWhenTheFileDoesNotSetIt(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	writeConfig(t, xdg, "runs = 3\n")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxRunsPerProject != 0 {
		t.Errorf("MaxRunsPerProject = %d, want the default 0", cfg.MaxRunsPerProject)
	}
	if got := cfg.ProjectRuns(); got != 3 {
		t.Errorf("ProjectRuns = %d, want runs %d", got, 3)
	}
}

// ProjectRuns is the one place that says what a value of 0 means, so no caller
// writes the fallback to runs of its own. A value above 0 is the limit itself,
// whether it is below runs or above it.
func TestProjectRunsIsTheKeyAndFallsBackToRuns(t *testing.T) {
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
