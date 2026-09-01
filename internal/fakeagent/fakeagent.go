// Package fakeagent interprets the script that dg-fake-agent runs. It is a real
// program rather than a test function, so a supervisor test exercises a real
// process start, a real dg finish call and a real timeout.
//
// A fake agent is what makes the queue, the supervisor, the reconcile and the
// inbox testable in seconds and for nothing. The prototype used a real agent
// run for each of those, which is why its faults went unfound.
package fakeagent

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// filePerm is the permission for files the script writes. These are the agent's
// work inside a worktree, not delegator's own data, so they take the ordinary
// permission a real agent would create rather than delegator's private 0600.
const filePerm = 0o644

// Run reads the script at path and executes each line. It returns the status
// the program should exit with.
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
