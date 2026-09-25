package run

import (
	"fmt"
	"time"
)

// StopGrace is the time allowed for graceful SIGTERM shutdown before SIGKILL
// on Unix.
const StopGrace = 5 * time.Second

// Stop terminates a supervisor and its child processes using the platform-
// specific implementation. An already-exited run is not an error.
//
// pid must come from a recently read live run. Nonpositive PIDs are rejected
// to avoid signalling the caller's process group.
func Stop(pid int, grace time.Duration) error {
	if pid <= 0 {
		return fmt.Errorf("%d is not the process id of a supervisor", pid)
	}
	return stop(pid, grace)
}
