// Package fakeagent runs the script of dg-fake-agent. The program is a real
// program and not a function of a test, so a test of the supervisor examines a
// real start of a program, a real call of dg finish, and a real timeout.
//
// Section 10.2 of the technical document says why: a fake agent makes the
// queue, the supervisor, the reconcile and the inbox testable in seconds and
// at no cost.
package fakeagent

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// filePerm is the permission of each file that the script writes. These are the
// work of the agent inside a worktree, and not the data of delegator, so
// section 11 does not cover them and they take the ordinary permission that a
// real agent gives a file it makes.
const filePerm = 0o644

// Run reads the script at path and does what each line says. It returns the
// status that the program must exit with.
func Run(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	lines := bufio.NewScanner(f)
	for lines.Scan() {
		line := strings.TrimSpace(lines.Text())
		if line == "" {
			continue
		}
		verb, rest, _ := strings.Cut(line, " ")
		switch verb {
		case "write":
			target, content, _ := strings.Cut(rest, " ")
			if err := os.WriteFile(target, []byte(content), filePerm); err != nil {
				return 0, err
			}
		case "run":
			cmd := exec.Command("sh", "-c", rest)
			cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
			if err := cmd.Run(); err != nil {
				return 0, fmt.Errorf("run %s: %w", rest, err)
			}
		case "exit":
			status, err := strconv.Atoi(strings.TrimSpace(rest))
			if err != nil {
				return 0, fmt.Errorf("exit takes a number: %w", err)
			}
			return status, nil
		default:
			return 0, fmt.Errorf("the script has no verb %q", verb)
		}
	}
	return 0, lines.Err()
}
