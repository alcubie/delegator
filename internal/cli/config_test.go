package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/testfix"
)

// A person who wants to change a value needs a file to open, and a misspelt
// key is refused, so the first command writes the file with each key at its
// default. Load reads that file, so the file and the defaults cannot come
// apart; the test asks Load rather than comparing text.
func TestACommandWritesTheConfigWhenItIsNotThere(t *testing.T) {
	configDir := testfix.XDGConfigDir(t)

	if _, err := runIn(t, t.TempDir(), t.TempDir(), "pause"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(configDir, "config.toml"))
	if err != nil {
		t.Fatalf("the command did not write config.toml: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load refuses the file the command wrote: %v\n%s", err, data)
	}
	if cfg != config.Default {
		t.Errorf("Load gives %+v from the file the command wrote, want the default %+v", cfg, config.Default)
	}

	// One comment for each key, so the person reads what a key does where
	// they change it. Each line that sets a key has a comment on the line
	// above it.
	lines := strings.Split(string(data), "\n")
	keys := 0
	for i, line := range lines {
		if !strings.Contains(line, "=") {
			continue
		}
		keys++
		if i == 0 || !strings.HasPrefix(lines[i-1], "#") {
			t.Errorf("the key on line %d has no comment above it:\n%s", i+1, data)
		}
	}
	if keys == 0 {
		t.Errorf("the file sets no key:\n%s", data)
	}
}
