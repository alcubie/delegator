// Package cli holds the body of each command of the person. The program cmd/dg
// reads the arguments and calls into here, so each command has a test that needs
// no terminal.
package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/alcubie/delegator/internal/atomicfile"
	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/queue"
	"github.com/alcubie/delegator/internal/ticket"
)

// dirPerm gives the permission of each directory that delegator makes. Section
// 11 says that a ticket can contain private data, so only its person can read
// it.
const dirPerm = 0o700

// Ticket makes a ticket for the project that holds workDir, and puts its id at
// the end of the queue. It gives the id.
func Ticket(dataDir, workDir, title string) (int, error) {
	root, err := project.Root(workDir)
	if err != nil {
		return 0, err
	}
	branch, err := project.DefaultBranch(root)
	if err != nil {
		return 0, err
	}
	projectDir, err := project.Create(dataDir, root, branch)
	if err != nil {
		return 0, err
	}

	// AddNew reserves the id and puts it in the queue in one step. A write
	// below that stops leaves an id in the queue that has no directory.
	id, err := queue.AddNew(dataDir)
	if err != nil {
		return 0, err
	}

	dir := filepath.Join(projectDir, "tickets", strconv.Itoa(id))
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return 0, err
	}

	t := &ticket.Ticket{
		Schema:  ticket.CurrentSchema,
		ID:      id,
		Title:   title,
		State:   "queued",
		Project: root,
		Created: time.Now().UTC(),
	}
	if err := t.SaveFields(dir); err != nil {
		return 0, err
	}

	// The person owns ticket.md, so delegator makes it and writes nothing in
	// it. Condition 2 puts the body there.
	if err := atomicfile.Write(filepath.Join(dir, "ticket.md"), nil, atomicfile.Perm); err != nil {
		return 0, err
	}
	return id, nil
}
