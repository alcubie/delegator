package run

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
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

// One run at a time: a run already active means nothing starts, however long
// the queue behind it.
func TestNextWithARunActiveStartsNothing(t *testing.T) {
	dataDir, first := queuedTicket(t, "the first")
	secondTicket(t, dataDir)
	if err := openStore(t, dataDir).Claim(first, "delegator/1-the-first"); err != nil {
		t.Fatal(err)
	}
	launch, marker := recordingLaunch(t)

	if err := Next(dataDir, launch); err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Errorf("a second run was started while one is active: ticket %s", waitFor(t, marker))
	}
}

// A run started by a command must not die with that command's terminal. The
// terminal's hangup and interrupt go to its session and its foreground process
// group, so the child is placed in a session of its own, and the test reads
// the group the child landed in from the child itself.
func TestNextStartsTheProgramInItsOwnSession(t *testing.T) {
	dataDir, _ := queuedTicket(t, "the first")
	marker := filepath.Join(t.TempDir(), "pgid")
	launch := func(id int64) *exec.Cmd {
		return exec.Command("sh", "-c", `ps -o pgid= -p $$ > "$1"`, "--", marker)
	}

	if err := Next(dataDir, launch); err != nil {
		t.Fatal(err)
	}

	if got := waitFor(t, marker); got == fmt.Sprint(syscall.Getpgrp()) {
		t.Errorf("the program is in the test's process group %s", got)
	}
}

// A child that keeps the standard streams of its parent holds them open: a
// shell waiting on dg's output would wait for the whole run. The run writes
// its own log, so the child gets no streams from the command that started it.
func TestNextGivesTheProgramNoneOfItsOwnStreams(t *testing.T) {
	dataDir, _ := queuedTicket(t, "the first")
	var started *exec.Cmd
	launch := func(id int64) *exec.Cmd {
		started = exec.Command("true")
		started.Stdin, started.Stdout, started.Stderr = os.Stdin, os.Stdout, os.Stderr
		return started
	}

	if err := Next(dataDir, launch); err != nil {
		t.Fatal(err)
	}

	if started.Stdin != nil || started.Stdout != nil || started.Stderr != nil {
		t.Errorf("the program kept a stream of its parent: in %v out %v err %v",
			started.Stdin != nil, started.Stdout != nil, started.Stderr != nil)
	}
}
