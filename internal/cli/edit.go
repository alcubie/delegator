// The command that changes the title and the prose of a ticket that waits. A
// person takes the two in an editor, which gets them as one text, in the form
// that dg ticket with no arguments takes. A caller that is not a person has the
// new text already and no editor to open, so it hands the text over in a flag.

package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/store"
)

// errEditTwoBodies shows that dg edit was given the prose twice. The prose has
// one source, and dg cannot tell which of the two the person meant.
var errEditTwoBodies = errors.New("dg edit takes the prose from --body or from --body-file, and got both")

// errEditorAndText shows that dg edit was told to open the editor and given the
// text as well. The editor holds the title and the prose, so a flag beside it is
// a second answer to the question the editor asks.
var errEditorAndText = errors.New("dg edit --editor takes the title and the prose from the editor, so it takes no --title, --body or --body-file")

// errEditNoForm shows that dg edit was given a ticket and nothing to put in it.
// The editor is one form of the command and not the form it falls back to,
// because a caller that is not a person has no editor to wait for.
var errEditNoForm = errors.New("dg edit takes --title, --body, --body-file or --editor, and got none")

// editCommand returns the command dg edit.
func editCommand(dataDir string, cfg *config.Config) *cobra.Command {
	var title, body, bodyFile string
	var useEditor bool
	cmd := &cobra.Command{
		Use:   "edit <id>",
		Short: "Change the title and the prose of a queued ticket.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := ticketArg(args[0])
			if err != nil {
				return err
			}
			flags := cmd.Flags()
			gaveTitle, gaveBody := flags.Changed("title"), flags.Changed("body")
			gaveFile := flags.Changed("body-file")
			gaveText := gaveTitle || gaveBody || gaveFile
			switch {
			case gaveBody && gaveFile:
				return errEditTwoBodies
			case useEditor && gaveText:
				return errEditorAndText
			case !useEditor && !gaveText:
				return errEditNoForm
			}
			// The prose is read before the store is open, so a path that names
			// nothing leaves the ticket as it was.
			if gaveFile {
				if body, err = ticketProse(cmd, bodyFile); err != nil {
					return err
				}
			}
			return withStore(dataDir, cfg, func(s *store.Store) error {
				if useEditor {
					return editFromEditor(s, id)
				}
				return editTicket(s, id, flagText(gaveTitle, title), flagText(gaveBody || gaveFile, body))
			})
		},
	}
	cmd.Flags().StringVar(&title, "title", "",
		"the new title of the ticket.")
	cmd.Flags().StringVar(&body, "body", "",
		"the new prose of the ticket.")
	cmd.Flags().StringVar(&bodyFile, "body-file", "",
		"the file that holds the new prose of the ticket.  - is the standard input.")
	cmd.Flags().BoolVar(&useEditor, "editor", false,
		"open $EDITOR on the title and the prose of the ticket.")
	return cmd
}

// flagText returns the text of a flag that the person gave, and nil for one
// that was not on the command line. An empty flag is text and not a flag that
// is missing, so the two cannot be told apart by the value alone.
func flagText(gave bool, text string) *string {
	if !gave {
		return nil
	}
	return &text
}

// editTicket writes a new title and new prose to a ticket of the queue. A title
// or a body that is nil leaves that part of the ticket as it is, because the
// caller gave dg nothing to put there.
func editTicket(s *store.Store, id int64, title, body *string) error {
	ticket, err := s.Ticket(id)
	if err != nil {
		return err
	}
	if ticket.Status != store.Queued {
		return refuseEdit(ticket)
	}

	if title != nil {
		name := strings.TrimSpace(*title)
		if name == "" {
			return ErrNoTitle
		}
		if err := s.SetTitle(id, name); err != nil {
			return err
		}
	}
	if body != nil {
		return os.WriteFile(proseFile(s.DataDir(), id), []byte(*body), filePerm)
	}
	return nil
}

// editFromEditor opens the editor of the person on the title and the prose of a
// ticket, and writes back what the editor gave: the first line to the column
// title, and each line below it to the file of prose.
func editFromEditor(s *store.Store, id int64) error {
	ticket, err := s.Ticket(id)
	if err != nil {
		return err
	}

	if ticket.Status != store.Queued {
		return refuseEdit(ticket)
	}

	path := proseFile(s.DataDir(), id)
	prose, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	before := ticket.Title + "\n\n" + string(prose)
	after, err := fromEditor(before)
	if err != nil {
		return err
	}

	// The person changed nothing, so nothing is written. The file is the file
	// of the person, and a write that puts back what was read is still a write
	// of text that delegator holds in memory.
	if after == before {
		return nil
	}

	title, body := splitTitle(after)
	if title == "" {
		return ErrNoTitle
	}
	if err := s.SetTitle(id, title); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(body), filePerm)
}

// refuseEdit returns the error for a ticket that dg edit cannot change. A run
// reads the ticket when it starts, so every state below queued belongs to a run
// that has already read it, and a change the agent cannot see leaves a report
// that answers a ticket which is not there any more. dg revise is the command
// that gives the work again with new prose.
func refuseEdit(ticket store.Ticket) error {
	if ticket.Status == store.Running {
		return fmt.Errorf("ticket %d is running: use dg revise after the run to change it", ticket.ID)
	}
	return fmt.Errorf("ticket %d is %s: only a ticket in the queue can be changed", ticket.ID, ticket.Status)
}
