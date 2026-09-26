// Cross-platform signature and build-selection checks for stop.

package run

import (
	"slices"
	"testing"
)

// stopFiles are the files that each give one stop, one for Unix and one for
// Windows.
var stopFiles = []string{"stop_unix.go", "stop_windows.go"}

// All platform implementations must expose the same signature.
func TestEachStopHasTheSameSignature(t *testing.T) {
	want := signature(t, stopFiles[0], "stop")
	if got := signature(t, stopFiles[1], "stop"); got != want {
		t.Errorf("%s declares %s, want the %s of %s", stopFiles[1], got, want, stopFiles[0])
	}
}

// Select exactly one implementation. _windows is an implicit constraint;
// _unix requires //go:build unix.
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

// Platform tests must be selected only for the platform they exercise.
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
