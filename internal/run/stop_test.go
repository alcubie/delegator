package run

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/testfix"
)

// shortGrace is the wait these tests give between the two signals. The real
// grace is seconds, which every test that reaches SIGKILL would spend waiting.
const shortGrace = 50 * time.Millisecond

// A signal to the group reaches the supervisor and each program the agent
// started, which is why the stop is a signal to the group and not to one
// process id.
func TestStopEndsEachProgramOfTheGroup(t *testing.T) {
	pgid := testfix.Group(t, `sleep 60 & : > "$1"; sleep 60`)

	if err := Stop(pgid, StopGrace); err != nil {
		t.Fatal(err)
	}

	testfix.WaitForGroupGone(t, pgid)
}

// SIGTERM comes first, so an agent with a handler for it closes what it opened
// rather than being taken away. Only a program that keeps the signal past the
// grace is killed.
func TestStopSendsSIGTERMBeforeSIGKILL(t *testing.T) {
	handled := filepath.Join(t.TempDir(), "handled")
	pgid := testfix.Group(t, fmt.Sprintf(
		`trap ': > %q; exit' TERM; : > "$1"; while :; do sleep 1; done`, handled))

	if err := Stop(pgid, StopGrace); err != nil {
		t.Fatal(err)
	}

	testfix.WaitForGroupGone(t, pgid)
	if _, err := os.Stat(handled); err != nil {
		t.Errorf("the handler of SIGTERM did not run: %v", err)
	}
}

// An agent that keeps SIGTERM would hold the ticket in running for ever. The
// stop waits for the grace and then sends SIGKILL, which no program can keep.
//
// The shell of the script keeps the signal and its sleep does not, so SIGTERM
// ends the sleep and the loop starts another: the group is still there when
// the grace is over. A script of one sleep would not do, because a shell runs
// the last command of its script in place of itself and the trap goes with it.
func TestStopKillsAProgramThatKeepsTheSignal(t *testing.T) {
	pgid := testfix.Group(t, `trap "" TERM; : > "$1"; while :; do sleep 1; done`)

	if err := Stop(pgid, shortGrace); err != nil {
		t.Fatal(err)
	}

	testfix.WaitForGroupGone(t, pgid)
}

// A run that ended between the read of its row and the signal is not an error.
// The command that sent the signal writes the state either way.
func TestStopOnARunThatIsAlreadyOver(t *testing.T) {
	if err := Stop(testfix.FreePID(t), shortGrace); err != nil {
		t.Errorf("err = %v, want nil", err)
	}
}

// A row with no process id reads as 0, and 0 as a group is the group of the
// caller: a stop that passed it on would signal dg itself and every program
// beside it. This test kills the test binary if the guard is not there.
func TestStopWithNoProcessID(t *testing.T) {
	for _, pid := range []int{0, -1} {
		if err := Stop(pid, shortGrace); err == nil {
			t.Errorf("Stop(%d) = nil, want an error", pid)
		}
	}
	if !testfix.GroupAlive(syscall.Getpgrp()) {
		t.Error("the group of the test was signalled")
	}
}
