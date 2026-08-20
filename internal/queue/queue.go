// Package queue keeps the order of the work. Section 7 of
// docs/TECHNICAL_DESIGN.md gives the files, and section 5 gives the lock.
package queue

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/alcubie/delegator/internal/atomicfile"
)

// queueFile is the name of the file that holds the ids, one on each line.
// Section 7 gives the name.
const queueFile = ".queue"

// Add puts the id at the end of the queue file, and makes the file if it is not
// present.
func Add(dataDir string, id int) error {
	path := filepath.Join(dataDir, queueFile)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, atomicfile.Perm)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(f, "%d\n", id); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// List returns a slice of IDs representing the current queue.
// It returns an empty slice if the file has not been created.
func List(dataDir string) ([]int, error) {
	path := filepath.Join(dataDir, queueFile)
	contents, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []int{}, nil
		}
		return nil, err
	}

	lines := strings.Split(string(contents), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	ids := make([]int, len(lines))
	for i, line := range lines {
		id, err := strconv.Atoi(line)
		if err != nil {
			return nil, fmt.Errorf("%s: line %d: %w", path, i+1, err)
		}
		ids[i] = id
	}

	return ids, nil
}
