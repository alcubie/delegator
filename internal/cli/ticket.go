package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/run"
	"github.com/alcubie/delegator/internal/store"
)

// filePerm is the permission of a ticket's prose file. A ticket can hold
// private data, so only the person who made it can read it.
const filePerm = 0o600

// ErrNoTitle shows that a ticket has no title. The inbox shows the title, and
// the branch of a run takes its name from it, so a ticket with no title is a
// ticket that a person cannot find again.
var ErrNoTitle = errors.New("the ticket must have a title")

// errTwoBodies shows that a ticket was given its prose twice. The prose has one
// source, and dg cannot tell which of the two the person meant.
var errTwoBodies = errors.New("dg ticket takes the prose from a body or from --body-file, and got both")

// ticketCommand makes a ticket and shows its id. With no argument it opens the
// editor of the person, with one it takes the title, and with two it takes the
// title and the prose. The flag --body-file takes the prose from a file
// instead.
func ticketCommand(dataDir, workDir string, cfg *config.Config) *cobra.Command {
	var projectDir string
	var bodyFile string
	var after []int64
	cmd := &cobra.Command{
		Use:   "ticket [title] [body]",
		Short: "Add a ticket. With no arguments, it opens $EDITOR.",
		Args:  cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if bodyFile != "" && len(args) > 1 {
				return errTwoBodies
			}
			// The prose is read before the store is open, so a path that
			// names nothing costs no ticket.
			var fileBody string
			if bodyFile != "" {
				var err error
				if fileBody, err = ticketProse(cmd, bodyFile); err != nil {
					return err
				}
			}
			dir, err := ticketProject(workDir, projectDir)
			if err != nil {
				return err
			}
			title := ""
			if len(args) > 0 {
				title = args[0]
			}
			return withStore(dataDir, cfg, func(s *store.Store) error {
				var id int64
				var err error
				switch {
				case bodyFile != "":
					id, err = Ticket(s, dir, title, fileBody, after...)
				case len(args) == 0:
					id, err = TicketFromEditor(s, dir, after...)
				case len(args) == 1:
					id, err = Ticket(s, dir, args[0], "", after...)
				case len(args) == 2:
					id, err = Ticket(s, dir, args[0], args[1], after...)
				default:
					return fmt.Errorf("dg ticket takes a title and a body, and got %d arguments", len(args))
				}
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), id)
				return run.Next(s, *cfg, launch)
			})
		},
	}
	cmd.Flags().StringVar(&projectDir, "project", "",
		"the directory of the project.  Defaults to current working directory.")
	cmd.Flags().StringVar(&bodyFile, "body-file", "",
		"the file that holds the prose of the ticket.  - is the standard input.")
	cmd.Flags().Int64SliceVar(&after, "after", nil,
		"the ticket ID this ticket depends on.  Can be repeated.")
	return cmd
}

// ticketProse reads the prose that --body-file names. A path of - is the
// standard input, as it is for git commit -F.
func ticketProse(cmd *cobra.Command, path string) (string, error) {
	if path == "-" {
		data, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ticketProject returns the directory whose project the new ticket belongs to.
// It is the directory dg runs in, and the flag --project names another one. A
// path that is not absolute is relative to the directory dg runs in, which is
// what the person who typed it meant.
//
// A directory that is not there is an error here rather than at project.Root,
// which asks git and would answer that a path nobody can find is not under
// version control.
func ticketProject(workDir, flag string) (string, error) {
	if flag == "" {
		return workDir, nil
	}
	dir := flag
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(workDir, dir)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", dir)
	}
	return dir, nil
}

// Ticket makes a ticket for the project that holds workDir, and puts it at the
// end of the queue. It returns the id. The body is the prose of the ticket, and
// it can be empty. The ids of dependsOn name the tickets the new one depends on.
//
// The caller gives the store, so that one command has one open: dg ticket
// writes the ticket and then starts the next run, and both are the work of the
// one command.
func Ticket(s *store.Store, workDir, title, body string, dependsOn ...int64) (int64, error) {
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

	projectID, err := s.ProjectID(root, branch)
	if err != nil {
		return 0, err
	}
	id, err := s.AddTicket(projectID, title, dependsOn...)
	if err != nil {
		return 0, err
	}

	// The person owns the prose after this write. Only dg revise adds to the
	// file, and no command writes it again from what it holds in memory.
	if err := os.WriteFile(proseFile(s.DataDir(), id), []byte(body), filePerm); err != nil {
		return 0, err
	}
	return id, nil
}
