//go:build unix

package run

import (
	"errors"
	"syscall"
)

// alive probes a positive PID with signal 0. Only ESRCH proves the process is
// gone; permission errors are treated as alive and left to the timeout.
// Reject nonpositive PIDs to avoid probing the caller's process group.
func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}
