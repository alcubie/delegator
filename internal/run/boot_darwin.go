package run

import (
	"time"

	"golang.org/x/sys/unix"
)

// bootTime returns the time at which the operating system started. macOS gives
// it as kern.boottime, which holds the moment of the boot and not a length, so
// a computer that slept needs no correction.
func bootTime() (time.Time, error) {
	boot, err := unix.SysctlTimeval("kern.boottime")
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(boot.Sec, int64(boot.Usec)*1000), nil
}
