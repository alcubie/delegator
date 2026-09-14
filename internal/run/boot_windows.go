// The boot time on Windows. A name that ends in _windows, _linux or _darwin is
// a build constraint of the go tool, so a build takes one of the three files
// and never two, and the bootTime that the reconcile calls is the one for the
// system the build is for.

package run

import (
	"time"

	"golang.org/x/sys/windows"
)

// bootTime returns the time at which the operating system started. Windows
// gives a length and not a moment: DurationSinceBoot is GetTickCount64 of
// kernel32, which counts the milliseconds since the start, so the boot time is
// now less that length. The count includes the time the computer slept, as
// btime on Linux does, which QueryUnbiasedInterruptTime does not.
func bootTime() (time.Time, error) {
	return time.Now().Add(-windows.DurationSinceBoot()), nil
}
