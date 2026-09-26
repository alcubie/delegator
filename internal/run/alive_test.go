// Cross-platform signature and build-selection checks for alive.

package run

import (
	"slices"
	"testing"
)

// aliveFiles are the files that each give one alive, one for Unix and one for
// Windows.
var aliveFiles = []string{"alive_unix.go", "alive_windows.go"}

// All platform implementations must expose the same signature.
func TestEachAliveHasTheSameSignature(t *testing.T) {
	want := signature(t, aliveFiles[0], "alive")
	if got := signature(t, aliveFiles[1], "alive"); got != want {
		t.Errorf("%s declares %s, want the %s of %s", aliveFiles[1], got, want, aliveFiles[0])
	}
}

// Select exactly one implementation. _windows is an implicit constraint;
// _unix requires //go:build unix.
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

// Platform tests must be selected only for the platform they exercise.
func TestOnlyAWindowsBuildTakesTheWindowsAliveTest(t *testing.T) {
	const test = "alive_windows_test.go"
	if taken := files(t, "windows", "TestGoFiles"); !slices.Contains(taken, test) {
		t.Errorf("the tests of a build for windows are %v, which hold no %s", taken, test)
	}
	if taken := files(t, "linux", "TestGoFiles"); slices.Contains(taken, test) {
		t.Errorf("the tests of a build for linux are %v, which hold %s", taken, test)
	}
}
