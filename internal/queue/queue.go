// Package queue keeps the order of the work. Three files hold it: .queue has
// the ids, .next-id has the counter, and .lock gives one writer at a time.
//
// The ids are unique for all projects, so the command dg show 4 is not
// ambiguous and the inbox can show each project together. Delegator has no
// server, and each dg command and each supervisor is a separate program, so the
// lock is the one thing that keeps two writers apart.
package queue

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/alcubie/delegator/internal/atomicfile"
)

// queueFile is the name of the file that holds the ids, one on each line. One
// id on each line lets the person change the order with an editor.
const queueFile = ".queue"

// nextIDFile is the name of the file that holds the next ticket ID.
// The file holds one number and no newline.  .queue ends each line with a newline so that
// Add() can append. .next-id has nothing to append so the decision was made to avoid a trailing newline.
const nextIDFile = ".next-id"

// lockFile gives the exclusive lock, and one writer operates at a time.
// The file has no content.  flock locks the inode, and this file is the one
// file that no write replaces, so its inode does not change.
const lockFile = ".lock"

// add puts the id at the end of the queue file, and makes the file if it is not
// present.  This requires the lock to already be held.
func add(dataDir string, id int) error {
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
// This doesn't use the lock because it will still return the current copy of the queue
// while a write is in progress.  With atomicfile.Write the queue can't be in a partially updated state.
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

func remove(dataDir string, id int) error {
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

// nextID returns the ID of the next ticket and increments the number in the storage file.
// It creates the file if it doesn't exist.
// This requires the lock to already be held.
func nextID(dataDir string) (int, error) {
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

// withLock runs fn while this program holds the exclusive lock.  Another
// program that asks for the lock waits until this one releases it.
// If the process dies then the lock is released.
func withLock(dataDir string, fn func() error) error {
	f, err := os.OpenFile(filepath.Join(dataDir, lockFile), os.O_CREATE|os.O_RDWR, atomicfile.Perm)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	return fn()
}

// Remove removes the ID from the queue no matter the position.
// If the ID does not exist, the queue remains unmodified.
// This blocks on a currently writing process.
func Remove(dataDir string, id int) error {
	return withLock(dataDir, func() error { return remove(dataDir, id) })
}

// AddNew reserves the next ID and puts it at the end of the queue.
// This blocks on a currently writing process.
func AddNew(dataDir string) (int, error) {
	var id int
	err := withLock(dataDir, func() error {
		var err error
		id, err = nextID(dataDir)
		if err != nil {
			return err
		}
		return add(dataDir, id)
	})
	return id, err
}
