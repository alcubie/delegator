package run

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/testfix"
)

func TestNextStartsTheFirstTicketOfTheQueue(t *testing.T) {
	dataDir, first := queuedTicket(t, "the first")
	testfix.SecondTicket(t, dataDir)
	launch, marker := testfix.RecordingLaunch(t)

	if err := Next(dataDir, launch); err != nil {
		t.Fatal(err)
	}

	if got := testfix.WaitFor(t, marker); got != fmt.Sprint(first) {
		t.Errorf("started ticket %s, want %d", got, first)
	}
}

// An empty queue starts nothing. The launch is what a supervisor would exec, so
// a start with no ticket would be a dg run with no id.
func TestNextWithAnEmptyQueueStartsNothing(t *testing.T) {
	dataDir := t.TempDir()
	testfix.OpenStore(t, dataDir)
	launch, marker := testfix.RecordingLaunch(t)

	if err := Next(dataDir, launch); err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Errorf("a program was started for an empty queue: %s", testfix.WaitFor(t, marker))
	}
}

// One run at a time: a run already active means nothing starts, however long
// the queue behind it.
func TestNextWithARunActiveStartsNothing(t *testing.T) {
	dataDir, first := queuedTicket(t, "the first")
	testfix.SecondTicket(t, dataDir)
	if err := testfix.OpenStore(t, dataDir).Claim(first, "delegator/1-the-first"); err != nil {
		t.Fatal(err)
	}
	launch, marker := testfix.RecordingLaunch(t)

	if err := Next(dataDir, launch); err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Errorf("a second run was started while one is active: ticket %s", testfix.WaitFor(t, marker))
	}
}

func TestNextWithAPausedQueueStartsNothing(t *testing.T) {
	dataDir, _ := queuedTicket(t, "the first")
	if err := testfix.OpenStore(t, dataDir).PauseQueue(); err != nil {
		t.Fatal(err)
	}
	launch, marker := testfix.RecordingLaunch(t)

	if err := Next(dataDir, launch); err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Errorf("a run was started while the queue is paused %s", testfix.WaitFor(t, marker))
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

	if got := testfix.WaitFor(t, marker); got == fmt.Sprint(syscall.Getpgrp()) {
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
