// Package cli is the command layer of dg: the cobra tree, the work behind each
// command, and the text a command writes to a terminal.
//
// cmd/dg only finds the directories and executes the tree, so every command can
// be tested here against a buffer rather than a terminal.
package cli

import (
	"fmt"
	"path/filepath"
	"strconv"
	"time"
)

// proseFile returns the path of the file that holds the prose of one ticket.
func proseFile(dataDir string, id int64) string {
	return filepath.Join(dataDir, "tickets", strconv.FormatInt(id, 10)+".md")
}

// elapsed returns the time from start to now as HH:MM:SS, which is what the row
// of a running ticket and the heading of dg show give for a run that is going.
// The zero time is no time, and gives the empty string.
//
// Each part keeps two figures, so a person who watches the inbox with
// watch -n 1 dg sees one column of digits that counts up rather than a field
// that changes width each minute. The hours take as many figures as they need:
// a run of 100 hours gives 100:00:00, because an hour that wrapped to 00 would
// say that a run of four days had just begun. A start in the future gives
// zero, which happens when the clock of the computer moves back.
func elapsed(start, now time.Time) string {
	if start.IsZero() {
		return ""
	}
	seconds := int(now.Sub(start).Seconds())
	if seconds < 0 {
		seconds = 0
	}
	return fmt.Sprintf("%02d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
}
