package run

import (
	"time"

	"golang.org/x/sys/windows"
)

// bootTime subtracts GetTickCount64 uptime from the current time. This
// includes time spent asleep, unlike QueryUnbiasedInterruptTime.
func bootTime() (time.Time, error) {
	return time.Now().Add(-windows.DurationSinceBoot()), nil
}
