package run

import (
	"errors"
	"fmt"
	"syscall"
	"time"
)

// StopGrace is how long Stop gives a run to end on SIGTERM before it sends
// SIGKILL. An agent writes a file and closes a connection as it stops, and a
// few seconds is long enough for that and short enough that a person who
// typed dg cancel does not wonder whether the command is working.
const StopGrace = 5 * time.Second

// stopPoll is how often Stop reads the group while it waits. A run that ends
// on the first signal ends in milliseconds, so the wait is over long before
// the grace in almost every cancel.
const stopPoll = 20 * time.Millisecond

// Stop ends the supervisor of a run and each program below it. detach gives
// each supervisor a session of its own, so the process id of the supervisor is
// also the id of its process group, and one signal to the group reaches the
// supervisor, the agent and every program the agent started. Section 6 of
// RUN_CONTROL.md is where this is decided.
//
// SIGTERM goes first, so a program that has a handler runs it. Stop then waits
// up to grace for the group to empty and sends SIGKILL to what is left, which
// no program can keep.
//
// A group that is already empty is not an error: the run ended between the
// read of its row and the signal, and the caller writes the state either way.
//
// pgid must be the process id of a supervisor that the caller has just read
// from a live run. A row that holds no process id reads as 0, and 0 as a group
// means the group of the caller, so this refuses it rather than signalling dg
// itself.
func Stop(pgid int, grace time.Duration) error {
	if pgid <= 0 {
		return fmt.Errorf("%d is not the process id of a supervisor", pgid)
	}
	if err := signal(pgid, syscall.SIGTERM); err != nil {
		return err
	}

	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if !groupAlive(pgid) {
			return nil
		}
		time.Sleep(stopPoll)
	}
	return signal(pgid, syscall.SIGKILL)
}

// signal sends one signal to a process group and reads ESRCH as the group
// having gone, which is the ordinary end of a run and not a fault.
func signal(pgid int, sig syscall.Signal) error {
	err := syscall.Kill(-pgid, sig)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

// groupAlive reports whether a process group still holds a program. Signal 0
// sends nothing, and to a group it gives ESRCH only when no program of the
// group is left, so one call answers for the supervisor and the agent below it
// together.
func groupAlive(pgid int) bool {
	return !errors.Is(syscall.Kill(-pgid, 0), syscall.ESRCH)
}
