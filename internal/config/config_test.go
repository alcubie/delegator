package config

import (
	"os"
	"path/filepath"
	"slices"
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

func TestLoadReadsASectionOfTheAgentsTable(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	writeConfig(t, xdg, "[agents.claude]\nargv = [\"my-acp\", \"--stdio\"]\nresume = [\"my-agent\", \"{session}\"]\n")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	agent, ok := cfg.Agents["claude"]
	if !ok {
		t.Fatalf("Agents = %v, want a section for claude", cfg.Agents)
	}
	if want := []string{"my-acp", "--stdio"}; !slices.Equal(agent.Argv, want) {
		t.Errorf("Argv = %v, want %v", agent.Argv, want)
	}
	if want := []string{"my-agent", "{session}"}; !slices.Equal(agent.Resume, want) {
		t.Errorf("Resume = %v, want %v", agent.Resume, want)
	}
}

// A person who names no agent gets no section, and the defaults say nothing
// about an agent: the table of the handler holds every agent delegator knows.
func TestLoadGivesNoAgentsWhenTheFileNamesNone(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Agents) != 0 {
		t.Errorf("Agents = %v, want none", cfg.Agents)
	}
}

func TestLoadRefusesAKeyOfAnAgentItDoesNotKnow(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	writeConfig(t, xdg, "[agents.claude]\nargvv = [\"my-acp\"]\n")

	_, err := Load()
	if err == nil {
		t.Fatal("Load of a key it does not know gave no error")
	}
	if !strings.Contains(err.Error(), "argvv") {
		t.Errorf("error %q does not name the key", err)
	}
}

// Windows has no XDG rule, and a person there keeps the config of a program
// below APPDATA. make check runs on Linux, so the system is a parameter of the
// unexported dir and the test names it.
func TestDirOnWindowsTakesAppData(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("APPDATA", `C:\Users\person\AppData\Roaming`)

	got, err := dir("windows")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(`C:\Users\person\AppData\Roaming`, "delegator"); got != want {
		t.Errorf("dir(windows) = %q, want %q", got, want)
	}
}

// Windows sets APPDATA for a person who signed in, so a run with it empty is a
// run with nothing to join, and joining nothing gives the relative path
// delegator, which would look for config.toml wherever the command was run.
func TestDirOnWindowsWithNoAppData(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("APPDATA", "")

	got, err := dir("windows")
	if err == nil {
		t.Fatalf("dir(windows) with no APPDATA = %q, want an error", got)
	}
	if !strings.Contains(err.Error(), "APPDATA") {
		t.Errorf("the error is %q, which does not name APPDATA", err)
	}
}

// A person who sets XDG_CONFIG_HOME means it on whichever system they are on,
// and the tests of this repository set it to hold their own directory, so it
// comes before the directory the system asks for. APPDATA is set here to name
// the one that would otherwise win.
func TestXDGConfigHomeWinsOnEachSystem(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/somewhere/config")
	t.Setenv("APPDATA", `C:\Users\person\AppData\Roaming`)

	for _, goos := range []string{"linux", "darwin", "windows"} {
		got, err := dir(goos)
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join("/somewhere/config", "delegator"); got != want {
			t.Errorf("dir(%s) = %q, want %q", goos, got, want)
		}
	}
}

// Linux and macOS keep the directory they had. APPDATA is set here because a
// person can set any variable on any system, and neither of these systems
// reads it.
func TestDirOffWindowsIsTheXDGDirectory(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("APPDATA", `C:\Users\person\AppData\Roaming`)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	for _, goos := range []string{"linux", "darwin"} {
		got, err := dir(goos)
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(home, ".config", "delegator"); got != want {
			t.Errorf("dir(%s) = %q, want %q", goos, got, want)
		}
	}
}

func TestLoadReadsRunner(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	writeConfig(t, xdg, "runner = \"acp\"\n")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Runner != RunnerACP {
		t.Errorf("Runner = %q, want %q", cfg.Runner, RunnerACP)
	}
}

func TestLoadWithNoRunnerGivesTheCommandLine(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Runner != RunnerCLI {
		t.Errorf("Runner = %q, want the default %q", cfg.Runner, RunnerCLI)
	}
}

// A runner delegator does not know would leave the person with a run that
// drove the agent some other way than the one they asked for.
func TestLoadRefusesARunnerItDoesNotKnow(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	writeConfig(t, xdg, "runner = \"mcp\"\n")

	_, err := Load()
	if err == nil {
		t.Fatal("Load accepted runner = \"mcp\"")
	}
	for _, want := range []string{"mcp", RunnerCLI, RunnerACP} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not name %q: %v", want, err)
		}
	}
}
