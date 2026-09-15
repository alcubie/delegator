// alive on Windows. A name that ends in _windows is a build constraint of the
// go tool, so this file is in a build for Windows and in no other, and the
// alive that the reconcile calls is the one for the system the build is for.

package run

import (
	"errors"

	"golang.org/x/sys/windows"
)

// alive reports whether a process id has a program. Windows has no signal 0.
// The exit code is the answer instead: OpenProcess gives a handle of the
// process, and GetExitCodeProcess gives STILL_ACTIVE, which is STATUS_PENDING,
// for a process that has not ended. The open alone is not the answer, because
// a handle that stays open holds the process of a program that ended, and
// OpenProcess opens that process and gives its code. The cost is that a
// program which ends with the code 259 reads as alive; the timeout covers it.
//
// The access is PROCESS_QUERY_LIMITED_INFORMATION, which is the least that
// GetExitCodeProcess takes, and which a program of another person gives, where
// PROCESS_QUERY_INFORMATION does not.
//
// Only ERROR_INVALID_PARAMETER says that the id is free, as only ESRCH says so
// on Unix. Any other refusal is a program that is there and will not answer
// this one, and the timeout covers it. An id of 0 or less is not asked about
// at all: a row that holds no process id reads as 0, and 0 is the idle process
// of the system.
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
