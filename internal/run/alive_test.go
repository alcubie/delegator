// The tests of alive that hold for every system. The answer of one system is
// tested by the tests of running in reconcile_test.go, which run wherever the
// build ran. These tests are about the two files together, and they run on
// any of them.

package run

import (
	"slices"
	"testing"
)

// aliveFiles are the files that each give one alive, one for Unix and one for
// Windows.
var aliveFiles = []string{"alive_unix.go", "alive_windows.go"}

// running calls alive for whichever system the build is for, so the two files
// must agree on what that call looks like. A Windows file that takes a uint32,
// or returns an error beside the answer, breaks the build of the package, and
// no test of this package can report it, because none of them runs on Windows.
func TestEachAliveHasTheSameSignature(t *testing.T) {
	want := signature(t, aliveFiles[0], "alive")
	if got := signature(t, aliveFiles[1], "alive"); got != want {
		t.Errorf("%s declares %s, want the %s of %s", aliveFiles[1], got, want, aliveFiles[0])
	}
}

// A build takes one alive and never two. The name alive_windows.go is the
// whole constraint of the Windows file, and the //go:build unix line is the
// whole constraint of the other, because _unix in a name says nothing to a
// build. A build for Windows that took both files, or neither, is the fault
// this finds.
func TestEachBuildTakesOneAlive(t *testing.T) {
	for _, system := range []struct{ goos, want, leave string }{
		{"windows", "alive_windows.go", "alive_unix.go"},
		{"linux", "alive_unix.go", "alive_windows.go"},
		{"darwin", "alive_unix.go", "alive_windows.go"},
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

// The test of the Windows alive goes to a build for Windows and to no other. A
// constraint that let it in here would put a test in make check that passes on
// signal 0 and says nothing about Windows.
func TestOnlyAWindowsBuildTakesTheWindowsAliveTest(t *testing.T) {
	const test = "alive_windows_test.go"
	if taken := files(t, "windows", "TestGoFiles"); !slices.Contains(taken, test) {
		t.Errorf("the tests of a build for windows are %v, which hold no %s", taken, test)
	}
	if taken := files(t, "linux", "TestGoFiles"); slices.Contains(taken, test) {
		t.Errorf("the tests of a build for linux are %v, which hold %s", taken, test)
	}
}
