package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/inbox"
)

// Explicit color modes override output detection, including pipes used by
// watch -c. Root flags must also work on subcommands.
func TestColorFlagDecidesTheColour(t *testing.T) {
	dataDir := t.TempDir()
	box := inbox.Inbox{QueueRunning: true}

	out, err := runIn(t, dataDir, t.TempDir(), "--color=always")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, colour(true, green, statusRunning)) {
		t.Errorf("dg --color=always into a pipe is not coloured:\n%q", out)
	}

	out, err = runIn(t, dataDir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("dg with no flag into a pipe is coloured:\n%q", out)
	}

	if _, err := runIn(t, dataDir, t.TempDir(), "pause", "--color=never"); err != nil {
		t.Errorf("dg pause does not take the flag: %v", err)
	}

	pty, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal to test with: %v", err)
	}
	defer pty.Close()
	if got := statusLine(pty, box, colourNever); got != statusRunning {
		t.Errorf("--color=never at a terminal gives %q, want %q", got, statusRunning)
	}
	if got, want := statusLine(pty, box, colourAuto), colour(true, green, statusRunning); got != want {
		t.Errorf("--color=auto at a terminal gives %q, want %q", got, want)
	}
}

// Color only the status word and reset immediately after it.
func TestColourWrapsOnlyTheWord(t *testing.T) {
	if got, want := colour(true, green, statusRunning), "Status: "+green+"Running"+plain; got != want {
		t.Errorf("coloured = %q, want %q", got, want)
	}
	if got := colour(false, green, statusRunning); got != statusRunning {
		t.Errorf("uncoloured = %q, want %q", got, statusRunning)
	}
}

// Use a PTY to distinguish terminals from buffers and files; skip if
// unavailable.
func TestIsTerminal(t *testing.T) {
	if isTerminal(&bytes.Buffer{}) {
		t.Error("a buffer is a terminal")
	}
	file, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if isTerminal(file) {
		t.Error("a file is a terminal")
	}

	pty, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal to test with: %v", err)
	}
	defer pty.Close()
	if !isTerminal(pty) {
		t.Error("a pseudo-terminal is not a terminal")
	}
}

func TestColorFlagRefusesAnotherValue(t *testing.T) {
	_, err := runIn(t, t.TempDir(), t.TempDir(), "--color=sometimes")
	if err == nil {
		t.Fatal("dg --color=sometimes gives no error")
	}
	for _, want := range []string{"sometimes", "always", "never", "auto"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not name %q: %v", want, err)
		}
	}
}

// Explicit flags override environment preferences; auto honors NO_COLOR
// before CLICOLOR_FORCE.
func TestColourEnvironmentDecidesAuto(t *testing.T) {
	box := inbox.Inbox{QueueRunning: true}
	coloured := colour(true, green, statusRunning)

	pty, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal to test with: %v", err)
	}
	defer pty.Close()

	t.Setenv("NO_COLOR", "")
	if got := statusLine(pty, box, colourAuto); got != statusRunning {
		t.Errorf("NO_COLOR at a terminal gives %q, want %q", got, statusRunning)
	}
	if got := statusLine(pty, box, colourAlways); got != coloured {
		t.Errorf("NO_COLOR with --color=always gives %q, want %q", got, coloured)
	}
	os.Unsetenv("NO_COLOR")

	t.Setenv("CLICOLOR_FORCE", "1")
	if got := statusLine(&bytes.Buffer{}, box, colourAuto); got != coloured {
		t.Errorf("CLICOLOR_FORCE into a pipe gives %q, want %q", got, coloured)
	}
	if got := statusLine(&bytes.Buffer{}, box, colourNever); got != statusRunning {
		t.Errorf("CLICOLOR_FORCE with --color=never gives %q, want %q", got, statusRunning)
	}
}

func TestHelpDescribesColor(t *testing.T) {
	out, err := runIn(t, t.TempDir(), t.TempDir(), "--help")
	if err != nil {
		t.Fatal(err)
	}
	var line string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "--color") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("the help does not describe --color:\n%s", out)
	}
	for _, want := range []string{"always", "never", "auto"} {
		if !strings.Contains(line, want) {
			t.Errorf("the line for --color does not name %q: %q", want, line)
		}
	}
}
