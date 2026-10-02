package testfix

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeT records expected fixture failures without failing or terminating the
// enclosing test.
type fakeT struct {
	failed  bool
	failure string
}

func (f *fakeT) Helper() {}

func (f *fakeT) Fatalf(format string, args ...any) {
	f.failed = true
	f.failure = fmt.Sprintf(format, args...)
}

type fakeWaitClock struct{ now time.Time }

func (c *fakeWaitClock) Now() time.Time { return c.now }

func (c *fakeWaitClock) Sleep(duration time.Duration) { c.now = c.now.Add(duration) }

func TestWaitForReturnsExistingMarkerWithoutWaiting(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ready")
	if err := os.WriteFile(marker, []byte(" ready\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := &fakeT{}
	clock := &fakeWaitClock{}

	if got := waitFor(f, marker, waitTimeout, clock); got != "ready" {
		t.Errorf("waitFor returned %q, want %q", got, "ready")
	}
	if !clock.now.IsZero() {
		t.Errorf("waitFor advanced clock to %v for an existing marker", clock.now)
	}
	if f.failure != "" {
		t.Errorf("waitFor reported unexpected failure: %s", f.failure)
	}
}

func TestWaitForFailsAtDefaultTimeout(t *testing.T) {
	if waitTimeout != 2*time.Second {
		t.Fatalf("waitTimeout = %v, want 2s", waitTimeout)
	}
	marker := filepath.Join(t.TempDir(), "never")
	f := &fakeT{}
	clock := &fakeWaitClock{}

	wantFailure := fmt.Sprintf("%s was not written", marker)
	waitFor(f, marker, waitTimeout, clock)

	if f.failure != wantFailure {
		t.Errorf("waitFor failure = %q, want %q", f.failure, wantFailure)
	}
	if got := clock.now.Sub(time.Time{}); got != waitTimeout {
		t.Errorf("waitFor expired after %v, want %v", got, waitTimeout)
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
