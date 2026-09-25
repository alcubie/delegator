package run

import (
	"time"

	"golang.org/x/sys/unix"
)

// bootTime reads kern.boottime, an absolute timestamp that needs no
// adjustment for time spent asleep.
func bootTime() (time.Time, error) {
	boot, err := unix.SysctlTimeval("kern.boottime")
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(boot.Sec, int64(boot.Usec)*1000), nil
}
