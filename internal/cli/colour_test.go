package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/inbox"
)

// The flag decides the colour, whatever the output is. always writes the
// codes into a pipe, which is what `watch -c` needs; never keeps them off a
// terminal; auto, the default, colours a terminal and nothing else. The flag
// sits on the root, so a subcommand takes it too.
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

// The check must say no for a buffer and for an ordinary file, and yes for a
// terminal. A pseudo-terminal stands in for the terminal, and the test is
// skipped where the system has none to give.
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

// A value that is none of the three is refused with the value and the three
// in the error, so a person who wrote --color=yes sees what to write instead.
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
