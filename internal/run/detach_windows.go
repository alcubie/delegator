// The attributes of a detached supervisor on Windows. A name that ends in
// _windows is a build constraint of the go tool, so this file is in a build
// for Windows and in no other, and the detachAttr that detach calls is the one
// for the system the build is for.

package run

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// detachAttr gives the attributes that take a supervisor out of the reach of
// the console that started it. Windows has no session, and the two creation
// flags together are what Setsid gives on Unix.
//
// CREATE_NEW_PROCESS_GROUP puts the child in a process group of its own. A
// Ctrl-C at the console goes to each process of the group of the console, and
// the child is in no such group, so the event does not reach it.
//
// DETACHED_PROCESS leaves the child with no console. A new process group alone
// is still attached to the console of its parent, and the close of a console
// ends each process that is attached to it, so the flag is what lets the
// person close the terminal while the run continues. The cost is that nothing
// can send the child a console event afterwards, because a console event
// reaches one console alone.
//
// The flags are the ones of golang.org/x/sys/windows, which the package uses
// already. The syscall of the standard library declares
// CREATE_NEW_PROCESS_GROUP and no DETACHED_PROCESS, and the two flags read
// better from one place.
func detachAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS}
}
