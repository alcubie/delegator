// The tests of alive on Windows. The name of the file is the build constraint,
// as it is for alive_windows.go, and make check runs on Linux, so nothing in
// make check runs these tests. TestOnlyAWindowsBuildTakesTheWindowsAliveTest
// holds them to Windows, TestTheWindowsAliveTypeChecks compiles them for
// Windows, and a person on Windows runs them.

package run

import (
	"os"
	"os/exec"
	"testing"

	"golang.org/x/sys/windows"
)

// ended runs a program that does nothing and returns its process id. The
// program has ended and nothing holds a handle of it, so Windows has taken the
// id back, as it takes back the id of a supervisor that stopped.
func ended(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("cmd", "/c", "exit")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

// The supervisor that a reconcile asks about is a program like this one. A
// process that has not ended gives the exit code STILL_ACTIVE, and that is the
// live supervisor the reconcile must leave alone.
func TestTheWindowsAliveFindsThisProgram(t *testing.T) {
	if !alive(os.Getpid()) {
		t.Errorf("the process id %d of this program is not alive", os.Getpid())
	}
}

// A process id that no process holds is the supervisor that stopped, and
// OpenProcess refuses it with ERROR_INVALID_PARAMETER.
func TestTheWindowsAliveOnAFreeProcessID(t *testing.T) {
	pid := ended(t)
	if alive(pid) {
		t.Errorf("the free process id %d is alive", pid)
	}
}

// OpenProcess opens a process that ended while a handle of it stays open, so
// the open alone answers nothing and the exit code is the check. The handle
// this test holds is what puts the process id in that state: the program has
// ended, and Windows cannot give the id to another program while the handle is
// open. A test with no handle of its own passes on the refusal of OpenProcess
// and says nothing about the exit code.
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
