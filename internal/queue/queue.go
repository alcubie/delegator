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

// nextIDFile is the name of the file that holds the next ticket ID.
// The file holds one number and no newline.  .queue ends each line with a newline so that
// Add() can append. .next-id has nothing to append so the decision was made to avoid a trailing newline.
const nextIDFile = ".next-id"

// Add puts the id at the end of the queue file, and makes the file if it is not
// present.
func Add(dataDir string, id int) error {
	f, err := os.OpenFile(queuePath(dataDir), os.O_APPEND|os.O_CREATE|os.O_WRONLY, atomicfile.Perm)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(f, "%d\n", id); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func queuePath(dataDir string) string {
	path := filepath.Join(dataDir, queueFile)
	return path
}

func nextIDPath(dataDir string) string {
	path := filepath.Join(dataDir, nextIDFile)
	return path
}

// List returns a slice of IDs representing the current queue.
// It returns an empty slice if the file has not been created.
func List(dataDir string) ([]int, error) {
	queuePath := queuePath(dataDir)
	contents, err := os.ReadFile(queuePath)
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
			return nil, fmt.Errorf("%s: line %d: %w", queuePath, i+1, err)
		}
		ids[i] = id
	}

	return ids, nil
}

// Remove removes the ID from the queue no matter the position.
// If the ID does not exist, the queue remains unmodified.
func Remove(dataDir string, id int) error {
	ids, err := List(dataDir)
	if err != nil {
		return err
	}

	var b strings.Builder
	for _, existing := range ids {
		if existing != id {
			fmt.Fprintf(&b, "%d\n", existing)
		}
	}

	if err := atomicfile.Write(queuePath(dataDir), []byte(b.String()), atomicfile.Perm); err != nil {
		return err
	}

	return nil
}

// NextID returns the ID of the next ticket and increments the number in the storage file.
// It creates the file if it doesn't exist.
func NextID(dataDir string) (int, error) {
	nextID, err := os.ReadFile(nextIDPath(dataDir))
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return 0, err
		}
		nextID = []byte("1")
	}

	id, err := strconv.Atoi(string(nextID))
	if err != nil {
		return 0, err
	}

	next := id + 1
	if err := atomicfile.Write(nextIDPath(dataDir), []byte(strconv.Itoa(next)), atomicfile.Perm); err != nil {
		return 0, err
	}

	return id, nil
}
