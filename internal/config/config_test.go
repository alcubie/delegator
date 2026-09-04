package config

import (
	"os"
	"path/filepath"
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
