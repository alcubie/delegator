// The tests of detachAttr on Windows. The name of the file is the build
// constraint, as it is for detach_windows.go, and make check runs on Linux, so
// nothing in make check runs these tests.
// TestOnlyAWindowsBuildTakesTheWindowsDetachTest holds them to Windows,
// TestTheWindowsDetachAttrTypeChecks compiles them for Windows, and a person
// on Windows runs them.

package run

import (
	"testing"

	"golang.org/x/sys/windows"
)

// A Ctrl-C at the console goes to each process of the process group of the
// console, and the supervisor of a run must outlive the command that a person
// typed there. CREATE_NEW_PROCESS_GROUP is what puts the supervisor in a group
// of its own, out of the reach of that event.
func TestTheWindowsDetachAttrMakesANewProcessGroup(t *testing.T) {
	if flags := detachAttr().CreationFlags; flags&windows.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Errorf("the creation flags are %#x, which hold no CREATE_NEW_PROCESS_GROUP", flags)
	}
}

// The close of a console ends each process that is attached to it, and a new
// process group is still attached to the console of its parent.
// DETACHED_PROCESS is what leaves the supervisor with no console at all, so the
// console of the person can close and the run continues.
func TestTheWindowsDetachAttrGivesNoConsole(t *testing.T) {
	if flags := detachAttr().CreationFlags; flags&windows.DETACHED_PROCESS == 0 {
		t.Errorf("the creation flags are %#x, which hold no DETACHED_PROCESS", flags)
	}
}
