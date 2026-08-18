// Package ticket reads and writes tickets. A ticket is one directory with two
// files. Delegator writes ticket.yaml. The person writes ticket.md.
package ticket

import (
	"os"
	"path/filepath"
	"time"

	"github.com/goccy/go-yaml"
)

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
	return t, nil
}
