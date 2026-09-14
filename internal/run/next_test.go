package run

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

// queueOf returns a data directory whose queue holds n tickets, and the id of
// each one in the order of the queue.
func queueOf(t *testing.T, n int) (string, []int64) {
	t.Helper()
	dataDir, first := queuedTicket(t, "the first")
	ids := []int64{first}
	for range n - 1 {
		ids = append(ids, testfix.SecondTicket(t, dataDir))
	}
	return dataDir, ids
}

// An empty queue starts nothing.
func TestNextWithAnEmptyQueueStartsNothing(t *testing.T) {
	dataDir := t.TempDir()
	s := testfix.OpenStore(t, dataDir)
	launch, marker := testfix.RecordingLaunch(t)

	if err := Next(s, config.Config{Runs: 1}, launch); err != nil {
		t.Fatal(err)
	}

	testfix.WaitForStarts(t, marker, 0)
}

func TestNextWithAPausedQueueStartsNothing(t *testing.T) {
	dataDir, _ := queuedTicket(t, "the first")
	s := testfix.OpenStore(t, dataDir)
	if err := s.PauseQueue(); err != nil {
		t.Fatal(err)
	}
	launch, marker := testfix.RecordingLaunch(t)

	if err := Next(s, config.Config{Runs: 1}, launch); err != nil {
		t.Fatal(err)
	}

	testfix.WaitForStarts(t, marker, 0)
}

// The limit of the person is how many tickets run at one time, and a queue
// with every slot free starts a supervisor for each of them. Each one claims a
// ticket of its own, so what Next decides is the count and not the ticket.
func TestNextStartsASupervisorForEachFreeSlot(t *testing.T) {
	dataDir, _ := queueOf(t, 3)
	launch, marker := testfix.RecordingLaunch(t)

	if err := Next(testfix.OpenStore(t, dataDir), config.Config{Runs: 3}, launch); err != nil {
		t.Fatal(err)
	}

	testfix.WaitForStarts(t, marker, 3)
}

// A supervisor with no ticket to claim stops, so a queue shorter than the free
// slots would start programs that do nothing. Next starts one for each ticket
// it can see instead.
func TestNextStartsNoMoreSupervisorsThanTheQueueHasTickets(t *testing.T) {
	dataDir, _ := queueOf(t, 2)
	launch, marker := testfix.RecordingLaunch(t)

	if err := Next(testfix.OpenStore(t, dataDir), config.Config{Runs: 3}, launch); err != nil {
		t.Fatal(err)
	}

	testfix.WaitForStarts(t, marker, 2)
}

// A run already active takes one of the slots, and the supervisors that start
// are for the slots it leaves.
func TestNextWithARunActiveStartsOneForEachSlotItLeaves(t *testing.T) {
	dataDir, ids := queueOf(t, 3)
	s := testfix.OpenStore(t, dataDir)
	if _, err := s.Claim(ids[0], "delegator/1-the-first"); err != nil {
		t.Fatal(err)
	}
	launch, marker := testfix.RecordingLaunch(t)

	if err := Next(s, config.Config{Runs: 3}, launch); err != nil {
		t.Fatal(err)
	}

	testfix.WaitForStarts(t, marker, 2)
}

// A ticket in ready holds its slot at any limit. Two tickets a person has not
// closed fill a limit of two, whatever the queue behind them holds.
func TestNextWithEverySlotHeldStartsNothing(t *testing.T) {
	dataDir, ids := queueOf(t, 3)
	s := testfix.OpenStore(t, dataDir)
	for _, id := range ids[:2] {
		if _, err := s.Claim(id, fmt.Sprintf("delegator/%d-ticket", id)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.FinishTicket(ids[0], "abc123"); err != nil {
		t.Fatal(err)
	}
	launch, marker := testfix.RecordingLaunch(t)

	if err := Next(s, config.Config{Runs: 2}, launch); err != nil {
		t.Fatal(err)
	}

	testfix.WaitForStarts(t, marker, 0)
}

// A supervisor calls Next as it ends, and what it starts is what its end
// freed. A run that failed leaves its ticket in no slot, so one supervisor
// starts for it, and the runs beside it keep their own slots.
func TestNextAfterARunEndsStartsOneForTheSlotItFreed(t *testing.T) {
	dataDir, ids := queueOf(t, 4)
	s := testfix.OpenStore(t, dataDir)
	for _, id := range ids[:2] {
		if _, err := s.Claim(id, fmt.Sprintf("delegator/%d-ticket", id)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ChangeStatus(ids[0], store.Failed); err != nil {
		t.Fatal(err)
	}
	launch, marker := testfix.RecordingLaunch(t)

	if err := Next(s, config.Config{Runs: 2}, launch); err != nil {
		t.Fatal(err)
	}

	testfix.WaitForStarts(t, marker, 1)
}

// A run started by a command must not die with that command's terminal. The
// terminal's hangup and interrupt go to its session and its foreground process
// group, so the child is placed in a session of its own, and the test reads
// the group the child landed in from the child itself.
func TestNextStartsTheProgramInItsOwnSession(t *testing.T) {
	dataDir, _ := queuedTicket(t, "the first")
	marker := filepath.Join(t.TempDir(), "pgid")
	launch := func() *exec.Cmd {
		return exec.Command("sh", "-c", `ps -o pgid= -p $$ > "$1"`, "--", marker)
	}

	if err := Next(testfix.OpenStore(t, dataDir), config.Config{Runs: 1}, launch); err != nil {
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
	launch := func() *exec.Cmd {
		started = exec.Command("true")
		started.Stdin, started.Stdout, started.Stderr = os.Stdin, os.Stdout, os.Stderr
		return started
	}

	if err := Next(testfix.OpenStore(t, dataDir), config.Config{Runs: 1}, launch); err != nil {
		t.Fatal(err)
	}

	if started.Stdin != nil || started.Stdout != nil || started.Stderr != nil {
		t.Errorf("the program kept a stream of its parent: in %v out %v err %v",
			started.Stdin != nil, started.Stdout != nil, started.Stderr != nil)
	}
}
