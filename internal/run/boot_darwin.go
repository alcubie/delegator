// The boot time on macOS. A name that ends in _darwin or _linux is a build
// constraint of the go tool, so a build takes this file or boot_linux.go and
// never both, and the bootTime that the reconcile calls is the one for the
// system the build is for.

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
