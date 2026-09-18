package cli

import (
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/testfix"
)

func TestConfigShowsDatabaseSettingsAndDescriptions(t *testing.T) {
	dataDir := t.TempDir()
	s := testfix.OpenStore(t, dataDir)
	if err := s.SetSetting("runs", "3"); err != nil {
		t.Fatal(err)
	}

	out, err := runIn(t, dataDir, t.TempDir(), "config")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"runs = 3  # the number", "timeout_minutes = 60  # the time"} {
		if !strings.Contains(out, want) {
			t.Errorf("dg config does not hold %q:\n%s", want, out)
		}
	}
}

func TestConfigListMatchesBareConfigAndKeepsDefinitionOrder(t *testing.T) {
	dataDir := t.TempDir()
	s := testfix.OpenStore(t, dataDir)
	for name, value := range map[string]string{
		"runs":                 "5",
		"timeout_minutes":      "45",
		"done_hours":           "6",
		"max_runs_per_project": "2",
		"default_agent":        "codex",
	} {
		if err := s.SetSetting(name, value); err != nil {
			t.Fatalf("set %s: %v", name, err)
		}
	}

	bare, err := runIn(t, dataDir, t.TempDir(), "config")
	if err != nil {
		t.Fatal(err)
	}
	listed, err := runIn(t, dataDir, t.TempDir(), "config", "list")
	if err != nil {
		t.Fatal(err)
	}
	if listed != bare {
		t.Errorf("dg config list output differs from dg config:\nlist:\n%s\nbare:\n%s", listed, bare)
	}

	want := "" +
		"runs = 5  # the number of tickets that can be Running or Ready at a time.\n\n" +
		"timeout_minutes = 45  # the time in minutes that a run can take before delegator stops it.\n\n" +
		"done_hours = 6  # the time in hours that a ticket stays in DONE at the top of the inbox after dg accept closes it. A value of 0 leaves DONE empty.\n\n" +
		"max_runs_per_project = 2  # the number of tickets of one project that can be Running or Ready at a time. A value of 0 is ignored and runs is used as the limit.\n\n" +
		"default_agent = codex  # the registered agent used for new runs.\n"
	if listed != want {
		t.Errorf("dg config list output = %q, want %q", listed, want)
	}
}

func TestConfigListShowsAnEmptyDefaultAgent(t *testing.T) {
	out, err := runIn(t, t.TempDir(), t.TempDir(), "config", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out, "default_agent =   # the registered agent used for new runs.\n") {
		t.Errorf("dg config list with no default agent ends with %q, want an empty default_agent", out)
	}
}

func TestConfigHelpNamesList(t *testing.T) {
	out, err := runIn(t, t.TempDir(), t.TempDir(), "config", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(out, "\n") {
		if fields := strings.Fields(line); len(fields) > 0 && fields[0] == "list" {
			if !strings.Contains(line, "every supported instance setting") {
				t.Errorf("the help line of dg config list is %q", line)
			}
			return
		}
	}
	t.Errorf("dg config help names no list command:\n%s", out)
}

func TestConfigGetShowsOneDatabaseSetting(t *testing.T) {
	dataDir := t.TempDir()
	s := testfix.OpenStore(t, dataDir)
	if err := s.SetSetting("done_hours", "6"); err != nil {
		t.Fatal(err)
	}

	out, err := runIn(t, dataDir, t.TempDir(), "config", "get", "done_hours")
	if err != nil {
		t.Fatal(err)
	}
	if out != "6\n" {
		t.Errorf("dg config get wrote %q, want %q", out, "6\\n")
	}
}

func TestConfigSetPersistsOneSetting(t *testing.T) {
	dataDir := t.TempDir()
	if _, err := runIn(t, dataDir, t.TempDir(), "config", "set", "runs", "4"); err != nil {
		t.Fatal(err)
	}

	out, err := runIn(t, dataDir, t.TempDir(), "config", "get", "runs")
	if err != nil {
		t.Fatal(err)
	}
	if out != "4\n" {
		t.Errorf("stored runs = %q, want 4", strings.TrimSpace(out))
	}
}

func TestConfigSetSelectsAnAvailableCustomDefaultAgent(t *testing.T) {
	dataDir := t.TempDir()
	command := executable(t, t.TempDir(), "mine-acp")
	if _, err := runIn(t, dataDir, t.TempDir(), "agents", "add", "mine", "--command", command); err != nil {
		t.Fatal(err)
	}
	if _, err := runIn(t, dataDir, t.TempDir(), "config", "set", "default_agent", "mine"); err != nil {
		t.Fatal(err)
	}

	out, err := runIn(t, dataDir, t.TempDir(), "config", "get", "default_agent")
	if err != nil {
		t.Fatal(err)
	}
	if out != "mine\n" {
		t.Errorf("stored default_agent = %q, want mine", strings.TrimSpace(out))
	}
}

func TestConfigSetRefusesAMissingAgentExecutableAndKeepsTheDefault(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	command := executable(t, t.TempDir(), "mine-acp")
	if _, err := runIn(t, dataDir, t.TempDir(), "agents", "add", "mine", "--command", command); err != nil {
		t.Fatal(err)
	}
	if _, err := runIn(t, dataDir, t.TempDir(), "config", "set", "default_agent", "mine"); err != nil {
		t.Fatal(err)
	}

	_, err := runIn(t, dataDir, t.TempDir(), "config", "set", "default_agent", "codex")
	want := "agent \"codex\" uses executable \"codex-acp\", but it was not found on PATH.\n" +
		"Install it or configure its executable with:\n" +
		"    dg agents add codex --path /path/to/codex-acp"
	if err == nil || err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}

	out, err := runIn(t, dataDir, t.TempDir(), "config", "get", "default_agent")
	if err != nil {
		t.Fatal(err)
	}
	if out != "mine\n" {
		t.Errorf("default_agent after failed change = %q, want mine", strings.TrimSpace(out))
	}
}

func TestConfigSetUnknownDefaultAgentSuggestsHowToAddIt(t *testing.T) {
	_, err := runIn(t, t.TempDir(), t.TempDir(), "config", "set", "default_agent", "missing")
	want := "agent \"missing\" is not registered.\n" +
		"Add it with:\n" +
		"    dg agents add missing --command /path/to/executable"
	if err == nil || err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
}

func TestConfigCommandsGiveUsefulErrors(t *testing.T) {
	dataDir := t.TempDir()
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"config", "get", "run"}, `unknown setting "run"`},
		{[]string{"config", "set", "run", "3"}, `unknown setting "run"`},
		{[]string{"config", "set", "runs", "many"}, "setting runs must be an integer"},
		{[]string{"config", "set", "runs", "0"}, "CHECK constraint failed: runs >= 1"},
	} {
		_, err := runIn(t, dataDir, t.TempDir(), tc.args...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("dg %v error = %v, want it to contain %q", tc.args, err, tc.want)
		}
	}
}
