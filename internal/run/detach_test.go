// The tests of detachAttr that hold for every system. The attributes of one
// system are tested by TestNextStartsTheProgramInItsOwnSession, which runs
// wherever the build ran. These tests are about the two files together, and
// they run on any of them.

package run

import (
	"os"
	"os/exec"
	"slices"
	"testing"
)

// detachFiles are the files that each give one detachAttr, one for Unix and
// one for Windows.
var detachFiles = []string{"detach_unix.go", "detach_windows.go"}

// detach calls detachAttr for whichever system the build is for, and gives
// what it returns to a *exec.Cmd, so the two files must agree on what that
// call looks like. A Windows file that returns a value rather than a pointer
// breaks the build of the package, and no test of this package can report it,
// because none of them runs on Windows.
func TestEachDetachAttrHasTheSameSignature(t *testing.T) {
	want := signature(t, detachFiles[0], "detachAttr")
	if got := signature(t, detachFiles[1], "detachAttr"); got != want {
		t.Errorf("%s declares %s, want the %s of %s", detachFiles[1], got, want, detachFiles[0])
	}
}

// A build takes one detachAttr and never two. The name detach_windows.go is
// the whole constraint of the Windows file, and the //go:build unix line is
// the whole constraint of the other, because _unix in a name says nothing to a
// build. A build for Windows that took both files, or neither, is the fault
// this finds.
func TestEachBuildTakesOneDetachAttr(t *testing.T) {
	for _, system := range []struct{ goos, want, leave string }{
		{"windows", "detach_windows.go", "detach_unix.go"},
		{"linux", "detach_unix.go", "detach_windows.go"},
		{"darwin", "detach_unix.go", "detach_windows.go"},
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

// The test of the Windows attributes goes to a build for Windows and to no
// other. A constraint that let it in here would put a test in make check that
// reads the Setsid of Linux and says nothing about Windows.
func TestOnlyAWindowsBuildTakesTheWindowsDetachTest(t *testing.T) {
	const test = "detach_windows_test.go"
	if taken := files(t, "windows", "TestGoFiles"); !slices.Contains(taken, test) {
		t.Errorf("the tests of a build for windows are %v, which hold no %s", taken, test)
	}
	if taken := files(t, "linux", "TestGoFiles"); slices.Contains(taken, test) {
		t.Errorf("the tests of a build for linux are %v, which hold %s", taken, test)
	}
}

// The test in detach_windows_test.go needs Windows, so make check cannot run
// it. A type check for Windows is what is left, and it reads the two files
// together, so a flag the Windows file does not declare fails here. The whole
// package cannot be built for Windows yet, because Stop is for Unix only,
// which is why this names the two files and not the package.
func TestTheWindowsDetachAttrTypeChecks(t *testing.T) {
	cmd := exec.Command("go", "vet", "detach_windows.go", "detach_windows_test.go")
	cmd.Env = append(os.Environ(), "GOOS=windows")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go vet for windows: %v: %s", err, out)
	}
}
