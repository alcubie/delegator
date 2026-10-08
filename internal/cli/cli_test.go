package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/testfix"
)

// repoBranch deliberately differs from main and master to expose hard-coded
// branch defaults.
const repoBranch = "trunk"

// testNow is the fixed clock used by rendering tests.
var testNow = time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)

// ticketIn makes one ticket in a data directory, with a store the test opens,
// as dg ticket create makes it with the store of the command.
func ticketIn(t *testing.T, dataDir, workDir, title, body string) (int64, error) {
	t.Helper()
	return Ticket(testfix.OpenStore(t, dataDir), workDir, title, body)
}

// ticketFromEditorIn creates an editor-based ticket using a test-owned store.
func ticketFromEditorIn(t *testing.T, dataDir, workDir string) (int64, error) {
	t.Helper()
	return TicketFromEditor(testfix.OpenStore(t, dataDir), workDir)
}

// proseFiles discovers description files without assuming their naming
// convention.
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

// proseOfTicket reads the prose of one ticket, for a test that made more than
// one and cannot use proseOf.
func proseOfTicket(t *testing.T, dataDir string, id int64) string {
	t.Helper()
	data, err := os.ReadFile(proseFile(dataDir, id))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// Keep minutes and seconds fixed-width while allowing hours above 99.
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

// Future starts after clock rollback must clamp to zero rather than print
// negative durations.
func TestElapsedWithAStartInTheFuture(t *testing.T) {
	if got := elapsed(testNow.Add(time.Hour), testNow); got != "00:00:00" {
		t.Errorf("elapsed = %q, want %q", got, "00:00:00")
	}
}
