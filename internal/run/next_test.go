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

// queueOf creates n queued tickets and returns their data directory and
// ordered IDs.
func queueOf(t *testing.T, n int) (string, []int64) {
	t.Helper()
	dataDir, first := queuedTicket(t, "the first")
	ids := []int64{first}
	for range n - 1 {
		ids = append(ids, testfix.SecondTicket(t, dataDir))
	}
	return dataDir, ids
}

// twoProjectQueue creates one ticket in each of two projects. Scheduling
// touches no Git state, so the second project needs no repository.
func twoProjectQueue(t *testing.T) string {
	t.Helper()
	dataDir, _ := queuedTicket(t, "the first")
	s := testfix.OpenStore(t, dataDir)
	other, err := s.AddProject("/projects/other", "main")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddTicket(other, "the other project's"); err != nil {
		t.Fatal(err)
	}
	return dataDir
}

// dependentQueue creates two queued tickets blocked by a running
// prerequisite.
func dependentQueue(t *testing.T) string {
	t.Helper()
	dataDir, first := queuedTicket(t, "the first")
	s := testfix.OpenStore(t, dataDir)
	projects, err := s.Projects()
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"depends", "depends as well"} {
		if _, err := s.AddTicket(projects[0].ID, title, first); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Claim(first, fmt.Sprintf("delegator/%d-the-first", first), testAgentID); err != nil {
		t.Fatal(err)
	}
	return dataDir
}

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

// Next decides the number of supervisors; each supervisor selects its own
// ticket.
func TestNextStartsASupervisorForEachFreeSlot(t *testing.T) {
	dataDir, _ := queueOf(t, 3)
	launch, marker := testfix.RecordingLaunch(t)

	if err := Next(testfix.OpenStore(t, dataDir), config.Config{Runs: 3}, launch); err != nil {
		t.Fatal(err)
	}

	testfix.WaitForStarts(t, marker, 3)
}

// Do not launch more supervisors than there are eligible tickets.
func TestNextStartsNoMoreSupervisorsThanTheQueueHasTickets(t *testing.T) {
	dataDir, _ := queueOf(t, 2)
	launch, marker := testfix.RecordingLaunch(t)

	if err := Next(testfix.OpenStore(t, dataDir), config.Config{Runs: 3}, launch); err != nil {
		t.Fatal(err)
	}

	testfix.WaitForStarts(t, marker, 2)
}

func TestNextWithARunActiveStartsOneForEachSlotItLeaves(t *testing.T) {
	dataDir, ids := queueOf(t, 3)
	s := testfix.OpenStore(t, dataDir)
	if _, err := s.Claim(ids[0], "delegator/1-the-first", testAgentID); err != nil {
		t.Fatal(err)
	}
	launch, marker := testfix.RecordingLaunch(t)

	if err := Next(s, config.Config{Runs: 3}, launch); err != nil {
		t.Fatal(err)
	}

	testfix.WaitForStarts(t, marker, 2)
}

// Ready work still consumes capacity, regardless of queue length.
func TestNextWithEverySlotHeldStartsNothing(t *testing.T) {
	dataDir, ids := queueOf(t, 3)
	s := testfix.OpenStore(t, dataDir)
	for _, id := range ids[:2] {
		if _, err := s.Claim(id, fmt.Sprintf("delegator/%d-ticket", id), testAgentID); err != nil {
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

// A failed run frees a slot without disturbing other active runs.
func TestNextAfterARunEndsStartsOneForTheSlotItFreed(t *testing.T) {
	dataDir, ids := queueOf(t, 4)
	s := testfix.OpenStore(t, dataDir)
	for _, id := range ids[:2] {
		if _, err := s.Claim(id, fmt.Sprintf("delegator/%d-ticket", id), testAgentID); err != nil {
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

// Regression: counting global slots while every project was full once
// launched over 1,500 supervisors that found no work and triggered
// replacements.
func TestNextWithEveryProjectAtItsLimitStartsNothing(t *testing.T) {
	dataDir, ids := queueOf(t, 3)
	s := testfix.OpenStore(t, dataDir)
	if _, err := s.Claim(ids[0], fmt.Sprintf("delegator/%d-ticket", ids[0]), testAgentID); err != nil {
		t.Fatal(err)
	}
	launch, marker := testfix.RecordingLaunch(t)

	if err := Next(s, config.Config{Runs: 4, MaxRunsPerProject: 1}, launch); err != nil {
		t.Fatal(err)
	}

	testfix.WaitForStarts(t, marker, 0)
}

// A queue blocked entirely by dependencies must start no supervisors.
func TestNextWithEveryTicketWaitingOnALinkStartsNothing(t *testing.T) {
	dataDir := dependentQueue(t)
	launch, marker := testfix.RecordingLaunch(t)

	if err := Next(testfix.OpenStore(t, dataDir), config.Config{Runs: 4}, launch); err != nil {
		t.Fatal(err)
	}

	testfix.WaitForStarts(t, marker, 0)
}

// Count one supervisor when only one project slot remains, even if two
// tickets are individually eligible.
func TestNextStartsOneSupervisorForAProjectWithOnePlace(t *testing.T) {
	dataDir, _ := queueOf(t, 2)
	launch, marker := testfix.RecordingLaunch(t)

	if err := Next(testfix.OpenStore(t, dataDir), config.Config{Runs: 4, MaxRunsPerProject: 1}, launch); err != nil {
		t.Fatal(err)
	}

	testfix.WaitForStarts(t, marker, 1)
}

func TestNextStartsOneSupervisorForEachProjectWithAPlace(t *testing.T) {
	dataDir := twoProjectQueue(t)
	launch, marker := testfix.RecordingLaunch(t)

	if err := Next(testfix.OpenStore(t, dataDir), config.Config{Runs: 4, MaxRunsPerProject: 1}, launch); err != nil {
		t.Fatal(err)
	}

	testfix.WaitForStarts(t, marker, 2)
}

// Read the child's session and group to verify isolation from the launching
// terminal's signals.
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

// Inherited standard streams would keep shell pipelines open for the whole
// run; detached supervisors must use their own logs.
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
