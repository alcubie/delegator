// Cross-platform signature and build-selection checks for detachAttr.

package run

import (
	"slices"
	"testing"
)

// detachFiles are the files that each give one detachAttr, one for Unix and
// one for Windows.
var detachFiles = []string{"detach_unix.go", "detach_windows.go"}

// All platform implementations must expose the same signature.
func TestEachDetachAttrHasTheSameSignature(t *testing.T) {
	want := signature(t, detachFiles[0], "detachAttr")
	if got := signature(t, detachFiles[1], "detachAttr"); got != want {
		t.Errorf("%s declares %s, want the %s of %s", detachFiles[1], got, want, detachFiles[0])
	}
}

// Select exactly one implementation. _windows is an implicit constraint;
// _unix requires //go:build unix.
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

// Platform tests must be selected only for the platform they exercise.
func TestOnlyAWindowsBuildTakesTheWindowsDetachTest(t *testing.T) {
	const test = "detach_windows_test.go"
	if taken := files(t, "windows", "TestGoFiles"); !slices.Contains(taken, test) {
		t.Errorf("the tests of a build for windows are %v, which hold no %s", taken, test)
	}
	if taken := files(t, "linux", "TestGoFiles"); slices.Contains(taken, test) {
		t.Errorf("the tests of a build for linux are %v, which hold %s", taken, test)
	}
}
