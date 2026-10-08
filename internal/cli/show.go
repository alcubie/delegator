// dg show renders ticket metadata and its description. --*-only flags expose
// individual values for shell commands; JSON output exposes the full
// structured result.

package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/run"
	"github.com/alcubie/delegator/internal/store"
)

// indent is the width of the space at the start of each line.
const indent = 2

// gap is the width of the space between a label and its text.
const gap = 2

// wrap breaks text at spaces up to width runes. Unbreakable words such as
// paths may exceed the width; blank lines are preserved.
func wrap(text string, width int) []string {
	var lines []string
	for _, para := range strings.Split(text, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		line := words[0]
		for _, word := range words[1:] {
			if len(line)+1+len(word) > width {
				lines = append(lines, line)
				line = word
				continue
			}
			line += " " + word
		}
		lines = append(lines, line)
	}
	// text that is empty gives one empty line, and the caller wants none
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

// ago formats a short relative age, returning empty for the zero time.
func ago(when time.Time, now time.Time) string {
	if when.IsZero() {
		return ""
	}
	switch d := now.Sub(when); {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// tilde replaces the user's home prefix with ~ without shortening the rest of
// the path.
func tilde(path, home string) string {
	if home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	rest, found := strings.CutPrefix(path, home+string(filepath.Separator))
	if !found {
		return path
	}
	return "~" + string(filepath.Separator) + rest
}

// labelWidth aligns values after the longest label, "depends on".
const labelWidth = 10

// ruleWidth is the width of the line below the title.
const ruleWidth = 67

// wrapWidth aligns wrapped field values with the title rule. Ticket
// descriptions retain their original line breaks.
const wrapWidth = ruleWidth - indent - labelWidth - gap

// writeField writes one label and its text, and puts each line below the first
// under the first.
func writeField(out io.Writer, label, text string) {
	lines := wrap(text, wrapWidth)
	fmt.Fprintf(out, "%*s%-*s%*s%s\n", indent, "", labelWidth, label, gap, "", lines[0])
	for _, line := range lines[1:] {
		fmt.Fprintf(out, "%*s%-*s%*s%s\n", indent, "", labelWidth, "", gap, "", line)
	}
}

// shortHashLen is how much of a hash one row shows. Git uses seven where that
// is enough to name one commit.
const shortHashLen = 7

// commitText returns a short hash and commit subject, falling back to the
// hash when Git cannot resolve it.
func commitText(root, hash string) string {
	short, subject, err := project.Commit(root, hash)
	if err != nil {
		return shortHash(hash)
	}
	// writeField wraps, and a wrap makes each run of spaces one space.
	return short + " " + subject
}

// shortHash cuts a hash to the length that a row shows.
func shortHash(hash string) string {
	if len(hash) <= shortHashLen {
		return hash
	}
	return hash[:shortHashLen]
}

// shown combines a stored ticket with its description file and text, existing
// worktree path, dependency IDs, and latest run start. Missing worktrees use
// an empty path; never-run tickets use the zero start time.
type shown struct {
	store.Ticket
	ProseFile string
	Prose     string
	Worktree  string
	DependsOn []int64
	Blocks    []int64
	Started   time.Time
	Agent     string
}

// writeHeading writes the title and the status, the time below the status, and
// the rule below them.
func writeHeading(out io.Writer, s shown, now time.Time) {
	// Show status age except for running tickets, which show elapsed run
	// time as in the inbox.
	var when string
	switch s.Status {
	case store.Running:
		when = elapsed(s.Started, now)
	default:
		when = ago(s.Changed, now)
	}
	heading := fmt.Sprintf("  #%d  %s", s.ID, s.Title)
	status := string(s.Status)
	pad := max(1, ruleWidth-len(heading)-len(status))
	fmt.Fprintf(out, "%s%s%s\n", heading, strings.Repeat(" ", pad), status)

	// Put time below the status to avoid crowding the title. Omit the
	// line when no timestamp is available.
	if when != "" {
		fmt.Fprintf(out, "%*s\n", ruleWidth, when)
	}
	fmt.Fprintf(out, "  %s\n", strings.Repeat("─", ruleWidth-2))
}

// writeFields writes the commit and the variables that connect the ticket to
// its work.
func writeFields(out io.Writer, s shown) {
	if s.Commit != "" {
		writeField(out, "commit", commitText(s.Project.Path, s.Commit))
		fmt.Fprintln(out)
	}

	home, _ := os.UserHomeDir()
	writeField(out, "project", tilde(s.Project.Path, home))
	writeField(out, "ticket", tilde(s.ProseFile, home))
	if s.Worktree != "" {
		writeField(out, "worktree", tilde(s.Worktree, home))
	}
	if s.Branch != "" {
		writeField(out, "branch", s.Branch)
	}
	if s.Session != "" {
		writeField(out, "session", s.Session)
	}
	if s.Agent != "" {
		writeField(out, "agent", s.Agent)
	}

	// Show all dependencies here; the inbox shows only unfinished
	// blockers.
	if len(s.DependsOn) > 0 {
		writeField(out, "depends on", ticketNames(s.DependsOn))
	}
	if len(s.Blocks) > 0 {
		writeField(out, "blocks", ticketNames(s.Blocks))
	}
}

// writeProse preserves Markdown line breaks so lists and paragraphs retain
// their formatting.
func writeProse(out io.Writer, s shown) {
	if prose := strings.TrimRight(s.Prose, "\n"); prose != "" {
		fmt.Fprintln(out)
		for _, line := range strings.Split(prose, "\n") {
			fmt.Fprintln(out, strings.TrimRight("  "+line, " "))
		}
	}
}

// writeTicket writes one ticket in full.
func writeTicket(out io.Writer, s shown, now time.Time) {
	writeHeading(out, s, now)
	writeFields(out, s)
	writeProse(out, s)
}

// ticketJSON is the structured show result. Missing scalar values are null;
// dependency links are always arrays.
type ticketJSON struct {
	ID        int64      `json:"id"`
	Title     string     `json:"title"`
	Status    string     `json:"status"`
	Project   string     `json:"project"`
	Ticket    string     `json:"ticket"`
	Worktree  *string    `json:"worktree"`
	Branch    *string    `json:"branch"`
	Session   *string    `json:"session"`
	Commit    *string    `json:"commit"`
	Created   *time.Time `json:"created"`
	Accepted  *time.Time `json:"accepted"`
	Prose     *string    `json:"prose"`
	DependsOn []int64    `json:"depends_on"`
	Blocks    []int64    `json:"blocks"`

	shown shown
}

// showResultSchema describes ticketJSON for RPC discovery. Nullable fields
// remain required so consumers can distinguish a known missing value from an
// older response that does not describe the field. Additional properties stay
// valid because adding fields does not change jsonSchema.
func showResultSchema() map[string]any {
	nullableString := map[string]any{"type": []string{"string", "null"}}
	timestamp := map[string]any{
		"type":   []string{"string", "null"},
		"format": "date-time",
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":       map[string]any{"type": "integer", "minimum": 1},
			"title":    map[string]any{"type": "string"},
			"status":   map[string]any{"type": "string", "enum": []string{string(store.Queued), string(store.Running), string(store.Ready), string(store.Failed), string(store.Done), string(store.Cancelled)}},
			"project":  map[string]any{"type": "string"},
			"ticket":   map[string]any{"type": "string"},
			"worktree": nullableString,
			"branch":   nullableString,
			"session": map[string]any{
				"type":        []string{"string", "null"},
				"description": "The opaque session identifier reported by the agent.",
			},
			"commit": map[string]any{
				"type":        []string{"string", "null"},
				"description": "The full Git object identifier recorded for the finished run.",
			},
			"created":  timestamp,
			"accepted": timestamp,
			"prose":    nullableString,
			"depends_on": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "integer", "minimum": 1},
			},
			"blocks": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "integer", "minimum": 1},
			},
		},
		"required": []string{
			"id", "title", "status", "project", "ticket", "worktree",
			"branch", "session", "commit", "created", "accepted", "prose",
			"depends_on", "blocks",
		},
		"additionalProperties": true,
	}
}

// nullable maps an empty string to nil.
func nullable(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// nullableTime maps the zero time to nil and formats other timestamps.
func nullableTime(at time.Time) *time.Time {
	if at.IsZero() {
		return nil
	}
	return &at
}

// ticketValue turns one shown ticket into the value that dg writes. shown is
// kept with it for the text renderer, and the exported fields are its JSON.
func ticketValue(s shown) ticketJSON {
	return ticketJSON{
		ID:        s.ID,
		Title:     s.Title,
		Status:    string(s.Status),
		Project:   s.Project.Path,
		Ticket:    s.ProseFile,
		Worktree:  nullable(s.Worktree),
		Branch:    nullable(s.Branch),
		Session:   nullable(s.Session),
		Commit:    nullable(s.Commit),
		Created:   nullableTime(s.Created),
		Accepted:  nullableTime(s.Accepted),
		Prose:     nullable(strings.TrimRight(s.Prose, "\n")),
		DependsOn: append([]int64{}, s.DependsOn...),
		Blocks:    append([]int64{}, s.Blocks...),
		shown:     s,
	}
}

// onlyField names the one field that a --*-only flag asks for. The empty
// value is no such flag, and dg show writes the whole ticket.
type onlyField string

const (
	onlyNone     onlyField = ""
	onlyProject  onlyField = "project"
	onlyTicket   onlyField = "ticket"
	onlyWorktree onlyField = "worktree"
	onlyBranch   onlyField = "branch"
	onlySession  onlyField = "session"
)

// writeOnly prints one raw field without labels, wrapping, or home-directory
// abbreviation. Shells do not expand ~ from a variable. Missing values
// produce no line.
func writeOnly(out io.Writer, s shown, only onlyField) {
	var value string
	switch only {
	case onlyProject:
		value = s.Project.Path
	case onlyTicket:
		value = s.ProseFile
	case onlyWorktree:
		value = s.Worktree
	case onlyBranch:
		value = s.Branch
	case onlySession:
		value = s.Session
	}
	if value == "" {
		return
	}
	fmt.Fprintln(out, value)
}

// showTicket loads metadata from the supplied store and description text from
// its data directory. Reusing the caller's store avoids a second open or
// reconciliation.
func showTicket(s *store.Store, id int64) (ticketJSON, error) {
	dataDir := s.DataDir()

	ticket, err := s.Ticket(id)
	if err != nil {
		return ticketJSON{}, err
	}
	t := shown{Ticket: ticket, ProseFile: proseFile(dataDir, id)}

	prose, err := os.ReadFile(t.ProseFile)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return ticketJSON{}, err
	}
	t.Prose = string(prose)

	lastRun, err := s.Run(id)
	if err != nil && !errors.Is(err, store.ErrNoRun) {
		return ticketJSON{}, err
	}
	t.Started = lastRun.StartedAt
	t.Agent = lastRun.Agent

	t.Worktree = run.WorktreePath(dataDir, id)
	if _, err := os.Stat(t.Worktree); err != nil {
		t.Worktree = ""
	}

	t.DependsOn, err = s.Dependencies(id)
	if err != nil {
		return ticketJSON{}, err
	}
	t.Blocks, err = s.Dependents(id)
	if err != nil {
		return ticketJSON{}, err
	}
	return ticketValue(t), nil
}

// onlyFlag implements mutually exclusive --*-only flags. All flags share
// field; pflag calls Set in command-line order, so the first selection claims
// it.
type onlyFlag struct {
	field *onlyField
	asks  onlyField
}

func (f onlyFlag) String() string { return strconv.FormatBool(*f.field == f.asks) }

// Type is bool, so the help gives the flag no value to write after it.
func (f onlyFlag) Type() string { return "bool" }

func (f onlyFlag) Set(value string) error {
	on, err := strconv.ParseBool(value)
	if err != nil {
		return err
	}
	if on && *f.field == onlyNone {
		*f.field = f.asks
	}
	return nil
}

// onlyFlags is each --*-only flag, with the words that its line of the help
// gives for it.
var onlyFlags = []struct {
	field onlyField
	what  string
}{
	{onlyProject, "the directory of the project"},
	{onlyTicket, "the file that holds the prose"},
	{onlyWorktree, "the directory the run works in"},
	{onlyBranch, "the branch of the work"},
	{onlySession, "the session of the last run"},
}

// showCommand displays an explicit ticket or the project's first ready
// ticket.
func showCommand(dataDir *string, workDir string, cfg *config.Config) *cobra.Command {
	var only onlyField
	var projectDir string
	cmd := &cobra.Command{
		Use:   "show [id]",
		Short: "Show the details of a ticket.",
		Long: "Show a ticket's status, project, worktree, branch, session, commit, dependency links, " +
			"run history, and prose. With no ID, show the first ready ticket for the selected project.",
		Example: `  dg show 42
  dg show
  dg show 42 --worktree-only`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withStore(*dataDir, cfg, func(s *store.Store) error {
				id, err := resolveTicketID(s, cfg, args, workDir, projectDir)
				if err != nil {
					return err
				}
				value, err := showTicket(s, id)
				if err != nil {
					return err
				}
				return writeValue(cmd.OutOrStdout(), value, false, func(out io.Writer) {
					if only != onlyNone {
						writeOnly(out, value.shown, only)
						return
					}
					writeTicket(out, value.shown, time.Now().UTC())
				})
			})
		},
	}
	cmd.Flags().StringVar(&projectDir, "project", "",
		"select the project whose first ready ticket to show when ID is omitted (default: current working directory)")
	for _, flag := range onlyFlags {
		name := string(flag.field) + "-only"
		usage := fmt.Sprintf("write only %s instead of the full ticket", flag.what)
		cmd.Flags().Var(onlyFlag{field: &only, asks: flag.field}, name, usage)
		// The flag takes no value, as a flag of cobra's own bool does.
		cmd.Flags().Lookup(name).NoOptDefVal = "true"
	}
	return rpcOperationCommand("show", cmd)
}
