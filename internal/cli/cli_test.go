package cli

import (
	"os"
	"path/filepath"
	"testing"
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
