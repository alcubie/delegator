package queue

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
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

func TestRemoveKeepsTheOrderOfTheOtherIDs(t *testing.T) {
	tests := []struct {
		name   string
		remove int
		want   []int
	}{
		{"first", 3, []int{2, 1}},
		{"middle", 2, []int{3, 1}},
		{"last", 1, []int{3, 2}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataDir := t.TempDir()
			for _, id := range []int{3, 2, 1} {
				if err := Add(dataDir, id); err != nil {
					t.Fatal(err)
				}
			}

			if err := Remove(dataDir, tt.remove); err != nil {
				t.Fatal(err)
			}

			got, err := List(dataDir)
			if err != nil {
				t.Fatal(err)
			}

			if !slices.Equal(got, tt.want) {
				t.Errorf("list = %v, want = %v", got, tt.want)
			}
		})
	}
}

func TestAddAfterRemoveKeepsTheLines(t *testing.T) {
	dataDir := t.TempDir()
	Add(dataDir, 3)
	Add(dataDir, 2)
	if err := Remove(dataDir, 2); err != nil {
		t.Fatal(err)
	}
	if err := Add(dataDir, 4); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(dataDir, ".queue"))
	if err != nil {
		t.Fatal(err)
	}

	want := "3\n4\n"
	if want != string(got) {
		t.Errorf("got = %s, want = %s", got, want)
	}
}

func TestNextIDReturnsNumberAndIncrements(t *testing.T) {
	dataDir := t.TempDir()
	nextIDPath := filepath.Join(dataDir, ".next-id")
	if err := os.WriteFile(nextIDPath, []byte("99"), 0o600); err != nil {
		t.Fatal(err)
	}

	id, err := NextID(dataDir)
	if err != nil {
		t.Fatal(err)
	}

	if id != 99 {
		t.Errorf("got = %d, want = %d", id, 99)
	}

	nextID, err := os.ReadFile(nextIDPath)
	if err != nil {
		t.Fatal(err)
	}

	if string(nextID) != "100" {
		t.Errorf("want = 100, got = %s", nextID)
	}
}

func TestNextIDReturnsOneForANewFileAndCreates(t *testing.T) {
	dataDir := t.TempDir()
	id, err := NextID(dataDir)
	if err != nil {
		t.Fatal(err)
	}

	if id != 1 {
		t.Errorf("got = %d, want = 1", id)
	}

	got, err := os.ReadFile(filepath.Join(dataDir, ".next-id"))
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != "2" {
		t.Errorf("got = %s, want = 2", got)
	}
}

func TestNextIDGivesNoDuplicates(t *testing.T) {
	dataDir := t.TempDir()
	const callers = 50
	ids := make([]int, callers)

	var wg sync.WaitGroup
	for i := range callers {
		wg.Go(func() {
			id, err := NextID(dataDir)
			if err != nil {
				t.Error(err)
				return
			}
			ids[i] = id
		})
	}
	wg.Wait()

	seen := map[int]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Errorf("id %d came back more than one time", id)
		}
		seen[id] = true
	}
}

func TestConcurrentRemoveLosesNoID(t *testing.T) {
	dataDir := t.TempDir()
	const ids = 50
	for i := range ids {
		if err := Add(dataDir, i+1); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup
	for i := range ids {
		wg.Go(func() {
			if err := Remove(dataDir, i+1); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()

	got, err := List(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("queue = %v, want empty", got)
	}
}

func TestConcurrentAddsWithRemovesLosesNoIDs(t *testing.T) {
	dataDir := t.TempDir()
	for i := range 50 {
		if err := Add(dataDir, i+1); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Go(func() {
			if err := Remove(dataDir, i+1); err != nil {
				t.Error(err)
			}
			if err := Add(dataDir, i+51); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()

	want := make([]int, 0, 50)
	for i := range 50 {
		want = append(want, i+51)
	}

	got, err := List(dataDir)
	if err != nil {
		t.Fatal(err)
	}

	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("queue = %v, want %v", got, want)
	}
}

func TestErroneousWriteLeavesFilesUnchanged(t *testing.T) {
	dataDir := t.TempDir()
	for range 2 {
		id, err := NextID(dataDir)
		if err != nil {
			t.Fatal(err)
		}
		if err = Add(dataDir, id); err != nil {
			t.Fatal(err)
		}
	}

	wantQueue, err := os.ReadFile(filepath.Join(dataDir, ".queue"))
	if err != nil {
		t.Fatal(err)
	}

	wantNextId, err := os.ReadFile(filepath.Join(dataDir, ".next-id"))
	if err != nil {
		t.Fatal(err)
	}

	if err = os.Chmod(dataDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dataDir, 0o755) })

	if err = Remove(dataDir, 1); err == nil {
		t.Error("Remove gave no error")
	}

	gotQueue, err := os.ReadFile(filepath.Join(dataDir, ".queue"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotQueue, wantQueue) {
		t.Errorf("got = %q, want = %q", gotQueue, wantQueue)
	}

	_, err = NextID(dataDir)
	if err == nil {
		t.Error("NextID gave no error")
	}

	gotNextId, err := os.ReadFile(filepath.Join(dataDir, ".next-id"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotNextId, wantNextId) {
		t.Errorf("got = %q, want = %q", gotNextId, wantNextId)
	}
}
