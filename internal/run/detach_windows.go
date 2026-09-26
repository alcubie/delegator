package run

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// detachAttr starts a separate process group without a console.
// CREATE_NEW_PROCESS_GROUP isolates Ctrl-C handling; DETACHED_PROCESS lets
// the supervisor survive closing the parent console. Without a console, it
// cannot later receive console control events. Both flags come from
// x/sys/windows because syscall lacks DETACHED_PROCESS.
func detachAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS}
}
