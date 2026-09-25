// Cross-platform signature and build-selection checks for bootTime.

package run

import (
	"slices"
	"testing"
)

// bootFiles are the files that each give one bootTime, one for each system that
// delegator builds for.
var bootFiles = []string{"boot_linux.go", "boot_darwin.go", "boot_windows.go"}

// All platform implementations must expose the same signature.
func TestEachBootTimeHasTheSameSignature(t *testing.T) {
	want := signature(t, bootFiles[0], "bootTime")
	for _, file := range bootFiles[1:] {
		if got := signature(t, file, "bootTime"); got != want {
			t.Errorf("%s declares %s, want the %s of %s", file, got, want, bootFiles[0])
		}
	}
}

// A Windows build must select the Windows implementation exclusively.
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

// Platform tests must be selected only for the platform they exercise.
func TestOnlyAWindowsBuildTakesTheWindowsBootTest(t *testing.T) {
	const test = "boot_windows_test.go"
	if taken := files(t, "windows", "TestGoFiles"); !slices.Contains(taken, test) {
		t.Errorf("the tests of a build for windows are %v, which hold no %s", taken, test)
	}
	if taken := files(t, "linux", "TestGoFiles"); slices.Contains(taken, test) {
		t.Errorf("the tests of a build for linux are %v, which hold %s", taken, test)
	}
}
