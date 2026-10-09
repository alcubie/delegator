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

// filePerm restricts ticket descriptions to their owner.
const filePerm = 0o600

// ErrNoTitle rejects a missing title, which is required for inbox display and
// branch naming.
var ErrNoTitle = errors.New("the ticket must have a title")

// errTwoBodies rejects competing description sources.
var errTwoBodies = errors.New("dg ticket create takes the prose from a body or from --body-file, and got both")

// errBodyAndNoBody rejects combining a description with --no-body.
var errBodyAndNoBody = errors.New("dg ticket create takes a body or --no-body, and got both")

// errNoBody requires an explicit description source or --no-body, preventing
// accidental tickets from a stray argument.
var errNoBody = errors.New("the ticket has no body: write one, or pass --no-body for a ticket that has none")

// ticketID is the value dg ticket create and dg accept write when they name a ticket.
type ticketID struct {
	ID int64 `json:"id"`
}

// ticketIDResultSchema describes the value returned by ticket and accept.
// Additional properties stay valid because adding fields does not change
// jsonSchema.
func ticketIDResultSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{"type": "integer", "minimum": 1},
		},
		"required":             []string{"id"},
		"additionalProperties": true,
	}
}

// ticketCommand is the namespace for ticket operations.
func ticketCommand(dataDir *string, workDir string, cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ticket",
		Short: "Work with tickets.",
		Long:  "Create, inspect, and edit tickets, their dependencies, and their recorded runs.",
		Example: `  dg ticket create "Add request tracing" --no-body
  dg ticket list
  dg ticket show 42
  dg ticket edit 42 --title "Handle expired sessions"
  dg ticket move 42 top
  dg ticket depend 42 --after 17
  dg ticket chat 42
  dg ticket runs 42`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(ticketCreateCommand(dataDir, workDir, cfg))
	cmd.AddCommand(listCommand(dataDir, workDir, cfg))
	cmd.AddCommand(searchCommand(dataDir, workDir, cfg))
	cmd.AddCommand(showCommand(dataDir, workDir, cfg))
	cmd.AddCommand(mapCommand(dataDir, workDir, cfg))
	cmd.AddCommand(editCommand(dataDir, cfg, "dg ticket edit"))
	cmd.AddCommand(moveCommand(dataDir, cfg, "dg ticket move"))
	cmd.AddCommand(dependCommand(dataDir, cfg, "dg ticket depend"))
	cmd.AddCommand(finishCommand(dataDir, cfg, "dg ticket finish"))
	cmd.AddCommand(acceptCommand(dataDir, workDir, cfg, "dg ticket accept"))
	cmd.AddCommand(cancelCommand(dataDir, cfg, "dg ticket cancel"))
	cmd.AddCommand(restartCommand(dataDir, cfg, "dg ticket restart"))
	cmd.AddCommand(chatCommand(dataDir, workDir, cfg))
	cmd.AddCommand(runsCommand(dataDir))
	return cmd
}

// ticketCompatibilityCommand identifies a retained root command and points
// help and remote discovery to its canonical command under ticket. The
// command remains otherwise identical and emits no migration warning.
func ticketCompatibilityCommand(canonical string, cmd *cobra.Command) *cobra.Command {
	cmd.Short += " (use " + canonical + ")"
	method := strings.ReplaceAll(strings.TrimPrefix(canonical, "dg "), " ", ".")
	cmd.Long += " The preferred command is `" + canonical + "`; the preferred JSON-RPC method is `" + method + "`."
	return cmd
}

// ticketTerminalCompatibilityCommand points a retained terminal-only command
// to its canonical ticket subcommand without advertising a remote method.
func ticketTerminalCompatibilityCommand(canonical string, cmd *cobra.Command) *cobra.Command {
	cmd.Short += " (use " + canonical + ")"
	cmd.Long += " The preferred command is `" + canonical + "`."
	return cmd
}

// ticketCreateCommand creates a ticket and prints its ID. No arguments open the
// editor; arguments supply title and description. --body-file reads the
// description from a file, and --no-body explicitly omits it.
func ticketCreateCommand(dataDir *string, workDir string, cfg *config.Config) *cobra.Command {
	var projectDir string
	var bodyFile string
	var noBody bool
	var after []int64
	cmd := &cobra.Command{
		Use:   "create [title] [body]",
		Short: "Add a ticket to a project queue.",
		Long: "Add a ticket to a project's queue. Supply a title and prose, read the prose " +
			"from --body-file, explicitly choose --no-body, or give no arguments to compose both fields in $EDITOR.",
		Example: `  dg ticket create "Remove the legacy endpoint" "Delete the handler and its tests."
  dg ticket create "Investigate the flaky test" --no-body
  dg ticket create "Implement the approved design" --body-file plan.md --after 41
  dg ticket create -- "-leading-dash" "Describe the change."`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if bodyFile != "" && len(args) > 1 {
				return errTwoBodies
			}
			if noBody && (len(args) > 1 || bodyFile != "") {
				return errBodyAndNoBody
			}
			if len(args) == 1 && bodyFile == "" && !noBody {
				return errNoBody
			}
			// Read the description before creating any ticket, so
			// an unreadable file leaves no record.
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
			return withStore(*dataDir, cfg, func(s *store.Store) error {
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
					return fmt.Errorf("dg ticket create takes a title and a body, and got %d arguments", len(args))
				}
				if err != nil {
					return err
				}
				if err := writeValue(cmd.OutOrStdout(), ticketID{ID: id}, false, func(out io.Writer) {
					fmt.Fprintln(out, id)
				}); err != nil {
					return err
				}
				return run.Next(s, *cfg, launchFrom(*dataDir, launch))
			})
		},
	}
	cmd.Flags().StringVar(&projectDir, "project", "",
		"add the ticket to the project in this directory (default: current working directory)")
	cmd.Flags().StringVar(&bodyFile, "body-file", "",
		"read the ticket prose from this file, or from standard input when the value is -")
	cmd.Flags().BoolVar(&noBody, "no-body", false,
		"add the ticket with no prose instead of requiring a body")
	cmd.Flags().Int64SliceVar(&after, "after", nil,
		"make the new ticket depend on this ticket ID (may be repeated or comma-separated)")
	return rpcOperationCommand("ticket", cmd)
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

// ticketProject resolves --project relative to the command's working
// directory, defaulting to that directory. It reports missing paths before
// Git would turn them into a misleading not-a-repository error.
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

// Ticket appends a ticket to the queue for the repository containing workDir
// and returns its ID. body may be empty; dependsOn lists prerequisite
// tickets. The supplied store is shared with the command's scheduling
// trigger.
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

	// Keep the description in an editable file, separate from ticket
	// metadata.
	if err := os.WriteFile(proseFile(s.DataDir(), id), []byte(body), filePerm); err != nil {
		return 0, err
	}
	return id, nil
}
