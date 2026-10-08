// dg edit updates a queued ticket's title and description through explicit
// text flags or an editor.

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

// errEditTwoBodies rejects competing description sources.
var errEditTwoBodies = errors.New("dg edit takes the prose from --body or from --body-file, and got both")

// errEditorAndText rejects mixing editor input with text flags.
var errEditorAndText = errors.New("dg edit --editor takes the title and the prose from the editor, so it takes no --title, --body or --body-file")

// errEditNoForm requires an explicit input method, so scripted calls do not
// unexpectedly open an editor.
var errEditNoForm = errors.New("dg edit takes --title, --body, --body-file or --editor, and got none")

// editCommand returns a fresh edit command whose examples use commandPath.
func editCommand(dataDir *string, cfg *config.Config, commandPath string) *cobra.Command {
	var title, body, bodyFile string
	var useEditor bool
	cmd := &cobra.Command{
		Use:   "edit <id>",
		Short: "Change the title and the prose of a queued ticket.",
		Long: "Change the title or prose of a queued ticket. Supply one or more text flags, " +
			"or use --editor to edit both fields in $EDITOR.",
		Example: fmt.Sprintf(`  %s 42 --title "Handle expired sessions"
  %s 42 --body-file revised-plan.md
  %s 42 --editor`, commandPath, commandPath, commandPath),
		Args: cobra.ExactArgs(1),
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
			// Read input before opening the store so an
			// unreadable file leaves the ticket unchanged.
			if gaveFile {
				if body, err = ticketProse(cmd, bodyFile); err != nil {
					return err
				}
			}
			return withStore(*dataDir, cfg, func(s *store.Store) error {
				if useEditor {
					return editFromEditor(s, id)
				}
				return editTicket(s, id, flagText(gaveTitle, title), flagText(gaveBody || gaveFile, body))
			})
		},
	}
	cmd.Flags().StringVar(&title, "title", "",
		"replace the ticket title with this value")
	cmd.Flags().StringVar(&body, "body", "",
		"replace the ticket prose with this value")
	cmd.Flags().StringVar(&bodyFile, "body-file", "",
		"read the replacement prose from this file, or from standard input when the value is -")
	cmd.Flags().BoolVar(&useEditor, "editor", false,
		"open $EDITOR to replace the title and prose instead of taking text flags")
	return rpcOperationCommand("edit", cmd)
}

// flagText distinguishes an omitted flag (nil) from an explicitly empty
// value.
func flagText(gave bool, text string) *string {
	if !gave {
		return nil
	}
	return &text
}

// editTicket updates a queued ticket, preserving title or body when the
// corresponding pointer is nil.
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

// editFromEditor edits the title and description together, then saves the
// first line as title and the remainder as description.
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

	// Avoid rewriting the description when the editor made no changes.
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

// refuseEdit rejects tickets outside the queue. Once a run has read the
// ticket, editing it could make the report describe different work.
func refuseEdit(ticket store.Ticket) error {
	if ticket.Status == store.Running {
		return fmt.Errorf("ticket %d is running: use dg revise after the run to change it", ticket.ID)
	}
	return fmt.Errorf("ticket %d is %s: only a ticket in the queue can be changed", ticket.ID, ticket.Status)
}
