package run

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// recordingLaunch returns a launch that writes the id it was given to a file,
// and the path of that file. It stands in for the launch of dg run, so a test
// sees which ticket Next chose without starting a real run.
func recordingLaunch(t *testing.T) (func(id int64) *exec.Cmd, string) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "started")
	return func(id int64) *exec.Cmd {
		return exec.Command("sh", "-c", `echo "$1" > "$2"`, "--", fmt.Sprint(id), marker)
	}, marker
}

// waitFor returns the content of path once it exists, or fails the test after
// a short wait. Next starts a program and does not wait for it, so the test
// has to.
func waitFor(t *testing.T, path string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil {
			return strings.TrimSpace(string(data))
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s was not written", path)
	return ""
}

// secondTicket adds one more ticket to the queue of a data directory that
// queuedTicket made, and returns its id.
func secondTicket(t *testing.T, dataDir string) int64 {
	t.Helper()
	s := openStore(t, dataDir)

	projects, err := s.Projects()
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.AddTicket(projects[0].ID, "the second")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestNextStartsTheFirstTicketOfTheQueue(t *testing.T) {
	dataDir, first := queuedTicket(t, "the first")
	secondTicket(t, dataDir)
	launch, marker := recordingLaunch(t)

	if err := Next(dataDir, launch); err != nil {
		t.Fatal(err)
	}

	if got := waitFor(t, marker); got != fmt.Sprint(first) {
		t.Errorf("started ticket %s, want %d", got, first)
	}
}

// An empty queue starts nothing. The launch is what a supervisor would exec, so
// a start with no ticket would be a dg run with no id.
func TestNextWithAnEmptyQueueStartsNothing(t *testing.T) {
	dataDir := t.TempDir()
	openStore(t, dataDir)
	launch, marker := recordingLaunch(t)

	if err := Next(dataDir, launch); err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Errorf("a program was started for an empty queue: %s", waitFor(t, marker))
	}
}
