// The stop of a run on Unix. The suffix _unix is not a build constraint of the
// go tool, as _linux and _windows are, so the constraint below is the whole of
// it: this file is in a build for Linux, for darwin and for the other Unix
// systems, and in no build for Windows.

//go:build unix

package run

import (
	"errors"
	"syscall"
	"time"
)

// stopPoll is how often stop reads the group while it waits. A run that ends
// on the first signal ends in milliseconds, so the wait is over long before
// the grace in almost every cancel.
const stopPoll = 20 * time.Millisecond

// stop ends a supervisor and each program below it with a signal to a process
// group. detach gives each supervisor a session of its own, so the process id
// of the supervisor is also the id of its process group, and one signal to the
// group reaches the supervisor, the agent and every program the agent started.
//
// SIGTERM goes first, so a program that has a handler runs it. stop then waits
// up to grace for the group to empty and sends SIGKILL to what is left, which
// no program can keep.
func stop(pid int, grace time.Duration) error {
	if err := signal(pid, syscall.SIGTERM); err != nil {
		return err
	}

	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if !groupAlive(pid) {
			return nil
		}
		time.Sleep(stopPoll)
	}
	return signal(pid, syscall.SIGKILL)
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
