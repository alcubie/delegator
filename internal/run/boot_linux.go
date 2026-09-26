package run

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

// procStat contains btime, the boot timestamp in Unix seconds. Reading an
// absolute timestamp avoids deriving boot time from an uptime duration with
// different sleep accounting.
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
