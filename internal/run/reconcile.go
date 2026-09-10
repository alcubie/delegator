package run

import (
	"errors"
	"os/exec"
	"syscall"
	"time"

	"github.com/alcubie/delegator/internal/store"
)

// Reconcile marks failed each ticket in running whose supervisor is gone, and
// gives its run an end time. Each command does it before its own work: a
// supervisor can stop with no report, from a crash or from a restart of the
// computer, and a supervisor that is not there can write nothing, so only a
// later command can correct the ticket it left behind.
//
// A reconcile that marked a run then starts the next ticket. One ticket runs
// at a time, so the ticket it just freed was holding every ticket below it,
// and after a restart of the computer this is what makes the queue go again.
// A reconcile that found nothing to correct starts nothing: the queue it
// looked at is one that a supervisor holds and will continue itself.
//
// timeout is how long a run may take, which the config of the person gives.
func Reconcile(s *store.Store, launch func() *exec.Cmd, timeout time.Duration) error {
	boot, err := bootTime()
	if err != nil {
		return err
	}
	now := time.Now()
	marked, err := s.Reconcile(func(r store.Run) bool { return running(r, boot, now, timeout) })
	if err != nil {
		return err
	}
	if marked == 0 {
		return nil
	}
	return Next(s, launch)
}

// running reports whether the supervisor of a run is still working. Section 4
// of RUN_CONTROL.md examines six ways to ask and selects this one, which is
// the process id with the boot time. A run is over when any of the three
// answers below says so:
//
//   - It began before the boot, whatever its process id says. Each id is free
//     after a restart of the computer, and the program that asks is itself a
//     dg, so an id that the system gave again would look like a live
//     supervisor and hold the ticket in running for ever.
//   - No program holds its process id. This is signal 0.
//   - It is past the timeout. That is the backstop for the one case the two
//     rules above leave: an id that the system gave again, inside one boot, to
//     a program that is alive.
//
// A timeout of nothing is no timeout. A person who writes 0 in the config asks
// for no limit on a run, and the other reading marks every run failed as it
// starts.
func running(r store.Run, boot, now time.Time, timeout time.Duration) bool {
	if r.StartedAt.Before(boot) {
		return false
	}
	if timeout > 0 && now.Sub(r.StartedAt) >= timeout {
		return false
	}
	return alive(r.PID)
}

// alive reports whether a process id has a program. Signal 0 sends nothing and
// gives the error that a real signal would give, which is how a program asks
// about another one.
//
// Only ESRCH says that the id is free. A program of another person answers
// with a permission error, and that is a program that is there; the timeout
// covers it. An id of 0 or less is not asked about at all: signal 0 to the id
// 0 reaches every program of the group of the caller, and a row that holds no
// process id reads as 0.
func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}
