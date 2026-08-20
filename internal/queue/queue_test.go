package queue

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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
	got, err := os.ReadFile(filepath.Join(dataDir, ".queue"))
	if err != nil {
		t.Fatal(err)
	}

	want := "1\n2\n"
	if string(got) != want {
		t.Errorf("queue = %q, want %q", got, want)
	}
}

func TestListGivesTheIDsInOrder(t *testing.T) {
	dataDir := t.TempDir()
	if err := Add(dataDir, 2); err != nil {
		t.Fatal(err)
	}
	if err := Add(dataDir, 1); err != nil {
		t.Fatal(err)
	}

	got, err := List(dataDir)
	if err != nil {
		t.Fatal(err)
	}

	want := []int{2, 1}
	if !slices.Equal(got, want) {
		t.Errorf("list = %v, want = %v", got, want)
	}
}

func TestListWhenQueueDoesNotExist(t *testing.T) {
	dataDir := t.TempDir()
	got, err := List(dataDir)
	if err != nil {
		t.Fatal(err)
	}

	want := []int{}
	if !slices.Equal(got, want) {
		t.Errorf("list = %v, want = %v", got, want)
	}
}

func TestListWithMalformedQueueFile(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, ".queue"), []byte("1\nabc\n2\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := List(dataDir)
	if !errors.Is(err, strconv.ErrSyntax) {
		t.Error("expected error")
	}
}
