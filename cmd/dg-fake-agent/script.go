package main

import (
	"bufio"
	"os"
	"strings"
)

// historyHeader begins the replay section, which continues to the end of the
// script.
const historyHeader = "history:"

// script separates prompt actions from replay history. Blank lines are
// ignored.
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
