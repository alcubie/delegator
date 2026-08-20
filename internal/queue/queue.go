// Package queue keeps the order of the work. Section 7 of
// docs/TECHNICAL_DESIGN.md gives the files, and section 5 gives the lock.
package queue

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/alcubie/delegator/internal/atomicfile"
)

// queueFile is the name of the file that holds the ids, one on each line.
// Section 7 gives the name.
const queueFile = "queue"

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
