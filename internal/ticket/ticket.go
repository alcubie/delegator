// Package ticket reads and writes tickets. A ticket is one directory with two
// files. Delegator writes ticket.yaml. The person writes ticket.md.
package ticket

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/goccy/go-yaml"
)

// CurrentSchema is the version of the format that this program writes.
const CurrentSchema = 1

// ErrUnknownSchema shows that a ticket comes from a later version of delegator.
var ErrUnknownSchema = errors.New("unknown schema")

// Ticket is one item of work.
type Ticket struct {
	Schema   int       `yaml:"schema"`
	ID       int       `yaml:"id"`
	Title    string    `yaml:"title"`
	State    string    `yaml:"state"`
	Project  string    `yaml:"project"`
	Branch   string    `yaml:"branch"`
	Worktree string    `yaml:"worktree"`
	Session  string    `yaml:"session"`
	Result   string    `yaml:"result"`
	Flags    string    `yaml:"flags"`
	Created  time.Time `yaml:"created"`
	Body     string    `yaml:"-"`
}

// Load reads the ticket that is in one directory.
func Load(dir string) (*Ticket, error) {
	data, err := os.ReadFile(filepath.Join(dir, "ticket.yaml"))
	if err != nil {
		return nil, err
	}
	t := &Ticket{}
	if err := yaml.Unmarshal(data, t); err != nil {
		return nil, err
	}
	if t.Schema < 1 || t.Schema > CurrentSchema {
		return nil, fmt.Errorf("%w %d: this version of delegator knows schema 1 to %d",
			ErrUnknownSchema, t.Schema, CurrentSchema)
	}

	body, err := os.ReadFile(filepath.Join(dir, "ticket.md"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	t.Body = string(body)
	return t, nil
}
