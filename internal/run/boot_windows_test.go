// Windows-only behavior tests. The cross-platform suite checks their build
// selection and type-checks them; execution requires Windows.

package run

import (
	"testing"
	"time"
)

// Two nearby calls must return a stable boot timestamp, not an increasing
// uptime duration.
func TestTheWindowsBootTimeIsOneMoment(t *testing.T) {
	first, err := bootTime()
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	second, err := bootTime()
	if err != nil {
		t.Fatal(err)
	}
	if moved := second.Sub(first).Abs(); moved > time.Second {
		t.Errorf("two boot times %s apart, %s then %s, want one moment", moved, first, second)
	}
}
