package testfix

import (
	"path/filepath"
	"testing"
	"time"
)

// fakeT stands for the *testing.T of a test that the fixture fails. A real one
// would fail the test that examines the failure, and Fatalf on a real one does
// not return, so the fixture would never reach its own end.
type fakeT struct{ failed bool }

func (f *fakeT) Helper() {}

func (f *fakeT) Fatalf(format string, args ...any) { f.failed = true }

func TestWaitForFailsAfterTwoSeconds(t *testing.T) {
	f := &fakeT{}

	start := time.Now()
	WaitFor(f, filepath.Join(t.TempDir(), "never"))
	elapsed := time.Since(start)

	if !f.failed {
		t.Fatal("WaitFor did not fail for a path that nothing writes")
	}
	if elapsed < 2*time.Second || elapsed >= 3*time.Second {
		t.Errorf("WaitFor failed after %v, want a failure after 2s", elapsed)
	}
}

func TestWaitForStartsSettlesForFiftyMilliseconds(t *testing.T) {
	_, marker := RecordingLaunch(t)

	start := time.Now()
	WaitForStarts(t, marker, 0)
	elapsed := time.Since(start)

	if elapsed < 50*time.Millisecond || elapsed >= 100*time.Millisecond {
		t.Errorf("WaitForStarts waited %v for a start that nothing makes, want a wait of 50ms", elapsed)
	}
}
