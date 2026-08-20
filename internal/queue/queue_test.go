package queue

import (
	"os"
	"path/filepath"
	"testing"
)

// Two ids, because one id cannot show that Add writes at the end. An Add that
// writes the file again each time gives the same result for one id.
func TestAddPutsTheIDAtTheEnd(t *testing.T) {
	dataDir := t.TempDir()
	for _, id := range []int{1, 2} {
		if err := Add(dataDir, id); err != nil {
			t.Fatal(err)
		}
	}

	// The name of the file is a literal. Section 7 gives it, and a change of the
	// name loses the queue of each person who has delegator now.
	got, err := os.ReadFile(filepath.Join(dataDir, "queue"))
	if err != nil {
		t.Fatal(err)
	}

	want := "1\n2\n"
	if string(got) != want {
		t.Errorf("queue = %q, want %q", got, want)
	}
}
