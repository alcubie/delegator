// The tests of bootTime that hold for every system. The value of one system is
// tested by TestBootTime, which runs wherever the build ran. These tests are
// about the three files together, and they run on any of them.

package run

import (
	"slices"
	"testing"
)

// bootFiles are the files that each give one bootTime, one for each system that
// delegator builds for.
var bootFiles = []string{"boot_linux.go", "boot_darwin.go", "boot_windows.go"}

// The reconcile calls bootTime for whichever system the build is for, so the
// three files must agree on what that call looks like. A Windows file that
// returns one value, or a Duration, breaks the build of the package, and no
// test of this package can report it, because none of them runs on Windows.
func TestEachBootTimeHasTheSameSignature(t *testing.T) {
	want := signature(t, bootFiles[0], "bootTime")
	for _, file := range bootFiles[1:] {
		if got := signature(t, file, "bootTime"); got != want {
			t.Errorf("%s declares %s, want the %s of %s", file, got, want, bootFiles[0])
		}
	}
}

// A build for Windows takes boot_windows.go and leaves the file of the system
// that the build runs on. The name of the file is the whole build constraint,
// so a Windows file that a build never reads is the fault this finds.
func TestAWindowsBuildTakesTheWindowsBootTime(t *testing.T) {
	taken := files(t, "windows", "GoFiles")
	if !slices.Contains(taken, "boot_windows.go") {
		t.Errorf("a build for windows takes %v, which holds no boot_windows.go", taken)
	}
	for _, file := range []string{"boot_linux.go", "boot_darwin.go"} {
		if slices.Contains(taken, file) {
			t.Errorf("a build for windows takes %v, which holds the %s of a second system", taken, file)
		}
	}
}

// The test of the Windows boot time goes to a build for Windows and to no
// other. A constraint that let it in here would put a test in make check that
// passes on the boot time of Linux and says nothing about Windows.
func TestOnlyAWindowsBuildTakesTheWindowsBootTest(t *testing.T) {
	const test = "boot_windows_test.go"
	if taken := files(t, "windows", "TestGoFiles"); !slices.Contains(taken, test) {
		t.Errorf("the tests of a build for windows are %v, which hold no %s", taken, test)
	}
	if taken := files(t, "linux", "TestGoFiles"); slices.Contains(taken, test) {
		t.Errorf("the tests of a build for linux are %v, which hold %s", taken, test)
	}
}
