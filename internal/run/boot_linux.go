package run

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

// procStat is the file that holds the boot time on Linux. The line "btime" of
// it gives the moment the operating system started, in seconds since the
// epoch. The file, and not /proc/uptime, because uptime is a length that a
// computer that slept does not count in the same way.
const procStat = "/proc/stat"

// bootTime returns the time at which the operating system started.
func bootTime() (time.Time, error) {
	data, err := os.ReadFile(procStat)
	if err != nil {
		return time.Time{}, err
	}
	for line := range strings.Lines(string(data)) {
		seconds, ok := strings.CutPrefix(strings.TrimSpace(line), "btime ")
		if !ok {
			continue
		}
		at, err := strconv.ParseInt(strings.TrimSpace(seconds), 10, 64)
		if err != nil {
			return time.Time{}, err
		}
		return time.Unix(at, 0), nil
	}
	return time.Time{}, errors.New(procStat + " holds no btime")
}
