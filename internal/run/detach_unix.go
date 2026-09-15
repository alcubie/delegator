// The attributes of a detached supervisor on Unix. The suffix _unix is not a
// build constraint of the go tool, as _linux and _windows are, so the
// constraint below is the whole of it: this file is in a build for Linux, for
// darwin and for the other Unix systems, and in no build for Windows.

//go:build unix

package run

import "syscall"

// detachAttr gives the attributes that take a supervisor out of the reach of
// the terminal that started it. The child gets a session of its own, so the
// hangup of a closed terminal and the interrupt of a Ctrl-C, which go to the
// session and to the foreground process group, do not reach it.
func detachAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
