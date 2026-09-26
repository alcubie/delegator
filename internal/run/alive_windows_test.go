// Windows-only behavior tests. The cross-platform suite checks their build
// selection and type-checks them; execution requires Windows.

package run

import (
	"os"
	"os/exec"
	"testing"

	"golang.org/x/sys/windows"
)

// ended returns the PID of a reaped process with no remaining handles.
func ended(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("cmd", "/c", "exit")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

func TestTheWindowsAliveFindsThisProgram(t *testing.T) {
	if !alive(os.Getpid()) {
		t.Errorf("the process id %d of this program is not alive", os.Getpid())
	}
}

func TestTheWindowsAliveOnAFreeProcessID(t *testing.T) {
	pid := ended(t)
	if alive(pid) {
		t.Errorf("the free process id %d is alive", pid)
	}
}

// Hold a handle after exit so OpenProcess still succeeds. This proves alive
// checks the exit code rather than treating an openable process as running.
func TestTheWindowsAliveOnAProcessThatEnded(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "exit")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(cmd.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}

	if alive(cmd.Process.Pid) {
		t.Errorf("the process id %d of a program that ended is alive", cmd.Process.Pid)
	}
}
