package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
