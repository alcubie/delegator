// The tests of stop that hold for every system. What one stop does is tested
// by the tests of that system, in stop_unix_test.go and in
// stop_windows_test.go. These tests are about the two files together, and they
// run on any of them.

package run

import (
	"slices"
	"testing"
)

// stopFiles are the files that each give one stop, one for Unix and one for
// Windows.
var stopFiles = []string{"stop_unix.go", "stop_windows.go"}

// Stop calls stop for whichever system the build is for, so the two files must
// agree on what that call looks like. A Windows file that takes no grace, or
// returns no error, breaks the build of the package, and no test of this
// package can report it, because none of them runs on Windows.
func TestEachStopHasTheSameSignature(t *testing.T) {
	want := signature(t, stopFiles[0], "stop")
	if got := signature(t, stopFiles[1], "stop"); got != want {
		t.Errorf("%s declares %s, want the %s of %s", stopFiles[1], got, want, stopFiles[0])
	}
}

// A build takes one stop and never two. The name stop_windows.go is the whole
// constraint of the Windows file, and the //go:build unix line is the whole
// constraint of the other, because _unix in a name says nothing to a build. A
// build for Windows that took both files, or neither, is the fault this finds.
func TestEachBuildTakesOneStop(t *testing.T) {
	for _, system := range []struct{ goos, want, leave string }{
		{"windows", "stop_windows.go", "stop_unix.go"},
		{"linux", "stop_unix.go", "stop_windows.go"},
		{"darwin", "stop_unix.go", "stop_windows.go"},
	} {
		taken := files(t, system.goos, "GoFiles")
		if !slices.Contains(taken, system.want) {
			t.Errorf("a build for %s takes %v, which holds no %s", system.goos, taken, system.want)
		}
		if slices.Contains(taken, system.leave) {
			t.Errorf("a build for %s takes %v, which holds the %s of a second system", system.goos, taken, system.leave)
		}
	}
}

// The tests of each stop go to a build for their own system. A constraint that
// let the Windows tests in here would put a test in make check that needs
// taskkill, and one that let the Unix tests into a build for Windows would
// stop that build, because they signal a process group.
func TestEachBuildTakesTheStopTestOfItsSystem(t *testing.T) {
	for _, system := range []struct{ goos, want, leave string }{
		{"windows", "stop_windows_test.go", "stop_unix_test.go"},
		{"linux", "stop_unix_test.go", "stop_windows_test.go"},
	} {
		taken := files(t, system.goos, "TestGoFiles")
		if !slices.Contains(taken, system.want) {
			t.Errorf("the tests of a build for %s are %v, which hold no %s", system.goos, taken, system.want)
		}
		if slices.Contains(taken, system.leave) {
			t.Errorf("the tests of a build for %s are %v, which hold %s", system.goos, taken, system.leave)
		}
	}
}
