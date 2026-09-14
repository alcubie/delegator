// The test of the boot time on Windows. The name of the file is the build
// constraint, as it is for boot_windows.go, and make check runs on Linux, so
// nothing in make check runs this test. TestOnlyAWindowsBuildTakesTheWindowsBootTest
// holds it to Windows, TestTheWindowsBootTimeTypeChecks compiles it for Windows,
// and a person on Windows runs it.

package run

import (
	"testing"
	"time"
)

// The boot time is one moment, so two calls a short time apart give one answer.
// Windows gives a length, and the subtraction that makes a moment of it is the
// step that a length used in the place of a moment passes: a value that grew
// between the two calls says that the code returned the length.
func TestTheWindowsBootTimeIsOneMoment(t *testing.T) {
	first, err := bootTime()
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	second, err := bootTime()
	if err != nil {
		t.Fatal(err)
	}
	if moved := second.Sub(first).Abs(); moved > time.Second {
		t.Errorf("two boot times %s apart, %s then %s, want one moment", moved, first, second)
	}
}
