// alive on Unix. The suffix _unix is not a build constraint of the go tool, as
// _linux and _windows are, so the constraint below is the whole of it: this
// file is in a build for Linux, for darwin and for the other Unix systems, and
// in no build for Windows.

//go:build unix

package run

import (
	"errors"
	"syscall"
)

// alive reports whether a process id has a program. Signal 0 sends nothing and
// gives the error that a real signal would give, which is how a program asks
// about another one.
//
// Only ESRCH says that the id is free. A program of another person answers
// with a permission error, and that is a program that is there; the timeout
// covers it. An id of 0 or less is not asked about at all: signal 0 to the id
// 0 reaches every program of the group of the caller, and a row that holds no
// process id reads as 0.
func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}
