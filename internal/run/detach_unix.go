//go:build unix

package run

import "syscall"

// detachAttr creates a new session so terminal hangups and foreground Ctrl-C
// signals do not reach the supervisor.
func detachAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
