// Package cli is the command layer of dg: the cobra tree, the work behind each
// command, and the text a command writes to a terminal.
//
// cmd/dg only finds the directories and executes the tree, so every command can
// be tested here against a buffer rather than a terminal.
package cli

import (
	"path/filepath"
	"strconv"
)

// proseFile returns the path of the file that holds the prose of one ticket.
func proseFile(dataDir string, id int64) string {
	return filepath.Join(dataDir, "tickets", strconv.FormatInt(id, 10)+".md")
}
