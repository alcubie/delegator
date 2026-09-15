package run

import (
	"fmt"
	"time"
)

// StopGrace is how long Stop gives a run to end on SIGTERM before it sends
// SIGKILL. An agent writes a file and closes a connection as it stops, and a
// few seconds is long enough for that and short enough that a person who
// typed dg cancel does not wonder whether the command is working.
const StopGrace = 5 * time.Second

// Stop ends the supervisor of a run and each program below it. What reaches
// them is the stop of the system the build is for. Section 6 of
// RUN_CONTROL.md is where this is decided.
//
// A run that is already over is not an error: it ended between the read of its
// row and the stop, and the caller writes the state either way.
//
// pid must be the process id of a supervisor that the caller has just read
// from a live run. A row that holds no process id reads as 0, and 0 as a group
// means the group of the caller, so this refuses it rather than signalling dg
// itself.
func Stop(pid int, grace time.Duration) error {
	if pid <= 0 {
		return fmt.Errorf("%d is not the process id of a supervisor", pid)
	}
	return stop(pid, grace)
}
