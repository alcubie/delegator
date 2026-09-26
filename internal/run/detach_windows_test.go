// Windows-only behavior tests. The cross-platform suite checks their build
// selection and type-checks them; execution requires Windows.

package run

import (
	"testing"

	"golang.org/x/sys/windows"
)

// A separate process group isolates the supervisor from console Ctrl-C.
func TestTheWindowsDetachAttrMakesANewProcessGroup(t *testing.T) {
	if flags := detachAttr().CreationFlags; flags&windows.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Errorf("the creation flags are %#x, which hold no CREATE_NEW_PROCESS_GROUP", flags)
	}
}

// A separate group still shares the console; DETACHED_PROCESS is required to
// survive console closure.
func TestTheWindowsDetachAttrGivesNoConsole(t *testing.T) {
	if flags := detachAttr().CreationFlags; flags&windows.DETACHED_PROCESS == 0 {
		t.Errorf("the creation flags are %#x, which hold no DETACHED_PROCESS", flags)
	}
}
