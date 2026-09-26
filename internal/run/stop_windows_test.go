// Windows-only behavior tests. The cross-platform suite checks their build
// selection and type-checks them; execution requires Windows.

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

// treeScript starts a child, writes its PID to $1, and waits. PowerShell
// Start-Process exposes the child PID, unlike cmd.
const treeScript = `$child = Start-Process powershell -ArgumentList '-NoProfile','-Command','Start-Sleep 60' -PassThru
Set-Content -Path '%s' -Value $child.Id
Start-Sleep 60`

// tree starts a supervisor-like parent and agent-like child. Wait for the
// child PID before returning so termination cannot race setup. A goroutine
// reaps the parent, and cleanup stops any surviving tree.
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

// waitForPID waits for a valid PID, not merely file existence, since creation
// can precede the write.
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

// waitForGone allows asynchronous process termination before reporting
// failure.
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

// taskkill /T must terminate both the supervisor and child.
func TestTheWindowsStopEndsTheSupervisorAndTheProgramBelowIt(t *testing.T) {
	parent, child := tree(t)

	if err := Stop(parent, StopGrace); err != nil {
		t.Fatal(err)
	}

	waitForGone(t, parent)
	waitForGone(t, child)
}

// taskkill exit code 128 for an already-gone PID is a successful stop.
func TestTheWindowsStopOnARunThatIsAlreadyOver(t *testing.T) {
	if err := Stop(ended(t), StopGrace); err != nil {
		t.Errorf("err = %v, want nil", err)
	}
}
