// Package cli holds the body of each command of the person. The program cmd/dg
// reads the arguments and calls into here, so each command has a test that needs
// no terminal.
package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/store"
)

// filePerm is the permission of each file that this package writes. A ticket
// can hold private data, so only the person who made it can read it.
const filePerm = 0o600

// ErrNoTitle shows that a ticket has no title. The inbox shows the title, and
// the branch of a run takes its name from it, so a ticket with no title is a
// ticket that a person cannot find again.
var ErrNoTitle = errors.New("the ticket has no title")

// proseFile returns the path of the file that holds the prose of one ticket.
func proseFile(dataDir string, id int64) string {
	return filepath.Join(dataDir, "tickets", strconv.FormatInt(id, 10)+".md")
}

// Ticket makes a ticket for the project that holds workDir, and puts it at the
// end of the queue. It returns the id. The body is the prose of the ticket, and
// it can be empty.
func Ticket(dataDir, workDir, title, body string) (int64, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return 0, ErrNoTitle
	}

	root, err := project.Root(workDir)
	if err != nil {
		return 0, err
	}
	branch, err := project.DefaultBranch(root)
	if err != nil {
		return 0, err
	}

	s, err := store.Open(dataDir)
	if err != nil {
		return 0, err
	}
	defer s.Close()

	projectID, err := s.ProjectID(root, branch)
	if err != nil {
		return 0, err
	}
	id, err := s.AddTicket(projectID, title)
	if err != nil {
		return 0, err
	}

	// The person owns the prose after this write. Only dg revise adds to the
	// file, and no command writes it again from what it holds in memory.
	if err := os.WriteFile(proseFile(dataDir, id), []byte(body), filePerm); err != nil {
		return 0, err
	}
	return id, nil
}
