//go:build unix

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

// shortGrace avoids production-length waits in tests that reach SIGKILL.
const shortGrace = 50 * time.Millisecond

// Stopping the group must terminate both supervisor and descendants.
func TestStopEndsEachProgramOfTheGroup(t *testing.T) {
	pgid := testfix.Group(t, `sleep 60 & : > "$1"; sleep 60`)

	if err := Stop(pgid, StopGrace); err != nil {
		t.Fatal(err)
	}

	testfix.WaitForGroupGone(t, pgid)
}

// Give signal handlers a chance to clean up before forced termination.
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

// The shell traps SIGTERM and starts a new sleep after its child exits,
// keeping the group alive until SIGKILL. A single sleep could replace the
// shell and lose the trap.
func TestStopKillsAProgramThatKeepsTheSignal(t *testing.T) {
	pgid := testfix.Group(t, `trap "" TERM; : > "$1"; while :; do sleep 1; done`)

	if err := Stop(pgid, shortGrace); err != nil {
		t.Fatal(err)
	}

	testfix.WaitForGroupGone(t, pgid)
}

func TestStopOnARunThatIsAlreadyOver(t *testing.T) {
	if err := Stop(testfix.FreePID(t), shortGrace); err != nil {
		t.Errorf("err = %v, want nil", err)
	}
}

// Reject PID zero: passing it through would signal the test runner's own
// group.
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
