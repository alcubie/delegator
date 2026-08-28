package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ErrNoCommand shows that dg got no command to run.
var ErrNoCommand = errors.New("no command")

// ErrUnknownCommand shows that dg got a command that it does not have.
var ErrUnknownCommand = errors.New("no such command")

// ErrTooManyArguments shows that a command got more arguments than it takes.
var ErrTooManyArguments = errors.New("too many arguments")

// DataDir gives the directory that holds the database and the prose of each
// ticket. XDG_DATA_HOME names it, and a person who has not set that variable
// gets the directory that the XDG specification asks for.
func DataDir() (string, error) {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "delegator"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "delegator"), nil
}

// ticketCommand makes a ticket and shows its id. With no argument it opens the
// editor of the person, with one it takes the title, and with two it takes the
// title and the prose.
func ticketCommand(out io.Writer, dataDir, workDir string, args []string) error {
	var id int64
	var err error
	switch len(args) {
	case 0:
		id, err = TicketFromEditor(dataDir, workDir)
	case 1:
		id, err = Ticket(dataDir, workDir, args[0], "")
	case 2:
		id, err = Ticket(dataDir, workDir, args[0], args[1])
	default:
		return fmt.Errorf("%w: dg ticket takes a title and a body", ErrTooManyArguments)
	}
	if err != nil {
		return err
	}
	fmt.Fprintln(out, id)
	return nil
}

// Run is the body of dg. The program cmd/dg gives it the arguments of the
// person and the place to write, so each command has a test that needs no
// terminal.
func Run(out io.Writer, dataDir, workDir string, args []string) error {
	if len(args) == 0 {
		return ErrNoCommand
	}
	switch args[0] {
	case "ticket":
		return ticketCommand(out, dataDir, workDir, args[1:])
	}
	return fmt.Errorf("%w: %s", ErrUnknownCommand, args[0])
}
