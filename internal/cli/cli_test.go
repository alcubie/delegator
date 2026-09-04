package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// repoBranch is the branch of each repository that these tests make. It is not
// main and not master, so a value that does not come from the repository is
// visible in a result.
const repoBranch = "trunk"

// proseFiles returns each file of prose that a command made. The test does not
// build the name itself, because the name is condition 5 of the ticket.
func proseFiles(t *testing.T, dataDir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dataDir, "tickets", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

// proseOf reads the one file of prose that a command made.
func proseOf(t *testing.T, dataDir string) string {
	t.Helper()
	files := proseFiles(t, dataDir)
	if len(files) != 1 {
		t.Fatalf("the files of prose are %v, want one", files)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// The duration of a run counts up while the person watches, so each part of it
// keeps a fixed width and the seconds are there. A run of more than 99 hours
// takes a wider field of hours rather than wrapping to zero.
func TestElapsed(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		start time.Time
		want  string
	}{
		{now, "00:00:00"},
		{now.Add(-7 * time.Second), "00:00:07"},
		{now.Add(-(14*time.Minute + 7*time.Second)), "00:14:07"},
		{now.Add(-(2*time.Hour + 3*time.Minute + 4*time.Second)), "02:03:04"},
		{now.Add(-100 * time.Hour), "100:00:00"},
		{time.Time{}, ""},
	}
	for _, test := range tests {
		if got := elapsed(test.start, now); got != test.want {
			t.Errorf("elapsed(%v) = %q, want %q", test.start, got, test.want)
		}
	}
}

// A clock that moves back, or a run that started on another computer, gives a
// start in the future. Zero is the honest answer, and a negative duration
// would print as a row of minus signs.
func TestElapsedWithAStartInTheFuture(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	if got := elapsed(now.Add(time.Hour), now); got != "00:00:00" {
		t.Errorf("elapsed = %q, want %q", got, "00:00:00")
	}
}
