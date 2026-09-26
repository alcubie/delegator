package store

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Production store opens belong to the CLI boundary; other packages receive a
// *Store. Inspect source to prevent hidden opens, allowing testfix to create
// test stores.
func TestOnlyCommandsAndFixturesOpenTheStore(t *testing.T) {
	root := filepath.Join("..", "..")
	allowed := []string{"internal/cli", "internal/testfix"}
	calls := []string{"store.With(", "store.Open("}

	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			if slices.Contains(allowed, filepath.Dir(rel)) {
				return nil
			}
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			lines := bufio.NewScanner(f)
			for n := 1; lines.Scan(); n++ {
				for _, call := range calls {
					if strings.Contains(lines.Text(), call) {
						t.Errorf("%s:%d calls %s; only a command in internal/cli opens the store", rel, n, call)
					}
				}
			}
			return lines.Err()
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
