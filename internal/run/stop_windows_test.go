// The tests of the stop of a run on Windows. The name of the file is the build
// constraint, as it is for stop_windows.go, and make check runs on Linux, so
// nothing in make check runs these tests.
// TestEachBuildTakesTheStopTestOfItsSystem holds them to Windows,
// TestTheWindowsFilesTypeCheck compiles them for Windows, and a person on
// Windows runs them.

package run

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// treeScript is the script of a tree: it starts a program of its own, writes
// the process id of that program to the file of $1, and then waits. PowerShell
// starts the child, because Start-Process gives back the process id of what it
// started and cmd does not.
const treeScript = `$child = Start-Process powershell -ArgumentList '-NoProfile','-Command','Start-Sleep 60' -PassThru
Set-Content -Path '%s' -Value $child.Id
Start-Sleep 60`

// tree starts a parent with a child below it and gives the process id of each.
// The parent stands for a supervisor and the child for the agent below it, so
// a test can stop the parent and see that both ended.
//
// The test waits for the file of the process id, because the tree is not whole
// until the child is there: a stop that arrives first reaches a parent with no
// child, and the test then says nothing about the child.
//
// The parent is a child of the test, so a goroutine waits on it. The tree is
// ended when the test ends, for a test whose own work leaves it alive.
func tree(t *testing.T) (parent, child int) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "child")
	cmd := exec.Command("powershell", "-NoProfile", "-Command", fmt.Sprintf(treeScript, file))
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go cmd.Wait()

	parent = cmd.Process.Pid
	t.Cleanup(func() {
		exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(parent)).Run()
	})
	return parent, waitForPID(t, file)
}

// waitForPID gives the process id that a tree wrote to a file, and waits for
// the file to hold one. A read that comes between the make of the file and the
// write of the text gives no number, so the wait is for a number and not for
// the file.
func waitForPID(t *testing.T, file string) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		text, err := os.ReadFile(file)
		if err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(text))); err == nil {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no process id is in %s", file)
	return 0
}

// waitForGone fails the test if a process is still there after a short wait.
// taskkill ends a process while the program that called it continues, so the
// test waits for the process to go rather than reading one time.
func waitForGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !alive(pid) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("the process %d is still there", pid)
}

// A stop must leave no program of the run behind. The supervisor is the
// process that the row of the run names, and the agent is a process below it,
// which taskkill /T reaches through the parent of each process.
func TestTheWindowsStopEndsTheSupervisorAndTheProgramBelowIt(t *testing.T) {
	parent, child := tree(t)

	if err := Stop(parent, StopGrace); err != nil {
		t.Fatal(err)
	}

	waitForGone(t, parent)
	waitForGone(t, child)
}

// A run that ended between the read of its row and the stop is not an error.
// taskkill gives the code 128 for a process id that no process holds, and dg
// cancel writes cancelled and the end time either way.
func TestTheWindowsStopOnARunThatIsAlreadyOver(t *testing.T) {
	if err := Stop(ended(t), StopGrace); err != nil {
		t.Errorf("err = %v, want nil", err)
	}
}
