//go:build unix

package run

import (
	"errors"
	"os"
	ossignal "os/signal"
	"syscall"
	"time"
)

// stopPoll controls how quickly Stop notices a group has exited during the
// grace period.
const stopPoll = 20 * time.Millisecond

// stop sends SIGTERM to the supervisor's process group, waits up to grace,
// then sends SIGKILL to any remaining members. Detach makes the supervisor
// PID its process-group ID. Descendants that stay in that group receive the
// same signals.
func stop(pid int, grace time.Duration) error {
	if pid == os.Getpid() && syscall.Getpgrp() == pid {
		term := make(chan os.Signal, 1)
		ossignal.Notify(term, syscall.SIGTERM)
		defer ossignal.Stop(term)
	}
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

// signal sends a process-group signal, treating ESRCH as an already-exited
// group.
func signal(pgid int, sig syscall.Signal) error {
	err := syscall.Kill(-pgid, sig)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

// groupAlive probes a process group with signal 0. ESRCH means no members
// remain.
func groupAlive(pgid int) bool {
	return !errors.Is(syscall.Kill(-pgid, 0), syscall.ESRCH)
}
