package main

import (
	"bufio"
	"os"
	"strings"
)

// historyHeader starts the section of a script that a load replays. It runs to
// the end of the file, so a script is the turn the agent takes and then the
// turn it already took.
const historyHeader = "history:"

// A script is the actions of a turn and the actions of the history. A blank
// line is skipped, so a script is laid out as the test that writes it likes.
type script struct {
	turn    []string
	history []string
}

// readScript reads the script at path.
func readScript(path string) (script, error) {
	f, err := os.Open(path)
	if err != nil {
		return script{}, err
	}
	defer f.Close()
	var s script
	section := &s.turn
	lines := bufio.NewScanner(f)
	for lines.Scan() {
		switch line := strings.TrimSpace(lines.Text()); line {
		case "":
		case historyHeader:
			section = &s.history
		default:
			*section = append(*section, line)
		}
	}
	return s, lines.Err()
}
