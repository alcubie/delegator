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
	for _, want := range []string{"runs = 3", "timeout_minutes = 60", "# runs is the number"} {
		if !strings.Contains(out, want) {
			t.Errorf("dg config does not hold %q:\n%s", want, out)
		}
	}
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

func TestConfigSetSelectsARegisteredDefaultAgent(t *testing.T) {
	dataDir := t.TempDir()
	if _, err := runIn(t, dataDir, t.TempDir(), "config", "set", "default_agent", "codex"); err != nil {
		t.Fatal(err)
	}

	out, err := runIn(t, dataDir, t.TempDir(), "config", "get", "default_agent")
	if err != nil {
		t.Fatal(err)
	}
	if out != "codex\n" {
		t.Errorf("stored default_agent = %q, want codex", strings.TrimSpace(out))
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
		{[]string{"config", "set", "default_agent", "missing"}, "invalid agent: missing"},
	} {
		_, err := runIn(t, dataDir, t.TempDir(), tc.args...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("dg %v error = %v, want it to contain %q", tc.args, err, tc.want)
		}
	}
}
