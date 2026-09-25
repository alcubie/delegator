package run

import (
	"errors"

	"golang.org/x/sys/windows"
)

// alive checks a positive PID with GetExitCodeProcess using
// PROCESS_QUERY_LIMITED_INFORMATION. Opening a handle alone is insufficient:
// Windows may retain an exited process while handles remain open.
//
// STILL_ACTIVE (259) means alive, but also matches a process that exited with
// code 259; the run timeout covers that ambiguity. Only
// ERROR_INVALID_PARAMETER proves the PID is absent. Other errors are treated
// as alive. Nonpositive PIDs are rejected, including the system idle process
// at PID 0.
func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return !errors.Is(err, windows.ERROR_INVALID_PARAMETER)
	}
	defer windows.CloseHandle(handle)
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return true
	}
	return code == uint32(windows.STATUS_PENDING)
}
