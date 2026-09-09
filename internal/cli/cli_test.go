package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/testfix"
)

// repoBranch is the branch of each repository that these tests make. It is not
// main and not master, so a value that does not come from the repository is
// visible in a result.
const repoBranch = "trunk"

// testNow is the moment that each test of this package writes text at. A time
// that a test puts before it or after it is a time the person would see beside
// that text, and the six copies of the same date these tests held said nothing
// that this one name does not.
var testNow = time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)

// ticketIn makes one ticket in a data directory, with a store the test opens,
// as dg ticket makes it with the store of the command.
func ticketIn(t *testing.T, dataDir, workDir, title, body string) (int64, error) {
	t.Helper()
	return Ticket(testfix.OpenStore(t, dataDir), workDir, title, body)
}

// ticketFromEditorIn makes one ticket from the editor of the person, with a
// store the test opens.
func ticketFromEditorIn(t *testing.T, dataDir, workDir string) (int64, error) {
	t.Helper()
	return TicketFromEditor(testfix.OpenStore(t, dataDir), workDir)
}

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
	tests := []struct {
		start time.Time
		want  string
	}{
		{testNow, "00:00:00"},
		{testNow.Add(-7 * time.Second), "00:00:07"},
		{testNow.Add(-(14*time.Minute + 7*time.Second)), "00:14:07"},
		{testNow.Add(-(2*time.Hour + 3*time.Minute + 4*time.Second)), "02:03:04"},
		{testNow.Add(-100 * time.Hour), "100:00:00"},
		{time.Time{}, ""},
	}
	for _, test := range tests {
		if got := elapsed(test.start, testNow); got != test.want {
			t.Errorf("elapsed(%v) = %q, want %q", test.start, got, test.want)
		}
	}
}

// A clock that moves back, or a run that started on another computer, gives a
// start in the future. Zero is the honest answer, and a negative duration
// would print as a row of minus signs.
func TestElapsedWithAStartInTheFuture(t *testing.T) {
	if got := elapsed(testNow.Add(time.Hour), testNow); got != "00:00:00" {
		t.Errorf("elapsed = %q, want %q", got, "00:00:00")
	}
}
