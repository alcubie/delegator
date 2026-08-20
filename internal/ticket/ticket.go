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
	"unicode/utf8"

	"github.com/goccy/go-yaml"

	"github.com/alcubie/delegator/internal/atomicfile"
)

// CurrentSchema is the version of the format that this program writes.
const CurrentSchema = 1

// MaxResult and MaxFlags are the limits, in characters, on the two fields that
// the agent writes. Section 6.4 gives the reason: a limit in a prompt to the
// agent does not operate.
const (
	MaxResult = 160
	MaxFlags  = 240
)

// ErrUnknownSchema shows that a ticket comes from a later version of delegator.
var ErrUnknownSchema = errors.New("unknown schema")

// ErrTooLong shows that a field from the agent is longer than its limit.
var ErrTooLong = errors.New("field is too long")

// Ticket is one item of work.
type Ticket struct {
	Schema   int       `yaml:"schema"`
	ID       int       `yaml:"id"`
	Title    string    `yaml:"title"`
	State    string    `yaml:"state"`
	Project  string    `yaml:"project"`
	Branch   string    `yaml:"branch,omitempty"`
	Worktree string    `yaml:"worktree,omitempty"`
	Session  string    `yaml:"session,omitempty"`
	Result   string    `yaml:"result,omitempty"`
	Flags    string    `yaml:"flags,omitempty"`
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

// SaveFields writes the fields of the ticket to ticket.yaml. It does not write
// ticket.md. Only the command that makes a ticket, and the command that adds
// prose to one, write that file.
func (t *Ticket) SaveFields(dir string) error {
	if err := checkLength("result", t.Result, MaxResult); err != nil {
		return err
	}
	if err := checkLength("flags", t.Flags, MaxFlags); err != nil {
		return err
	}

	data, err := yaml.Marshal(t)
	if err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(dir, "ticket.yaml"), data, atomicfile.Perm)
}

// checkLength refuses a value that is longer than its limit. It counts
// characters, and not bytes, because the limit is what the person reads.
func checkLength(name, value string, limit int) error {
	if n := utf8.RuneCountInString(value); n > limit {
		return fmt.Errorf("%w: %s has %d characters, and the limit is %d",
			ErrTooLong, name, n, limit)
	}
	return nil
}
