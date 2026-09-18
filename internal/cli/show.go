// The text of one ticket for a terminal. dg show gives a person the fields that
// a run wrote, the four variables that connect the ticket to its work, and the
// prose that the person wrote. A --*-only flag gives one of those values on a
// line of its own, for the person who is writing another command line with it,
// and dg rpc gives every field to a script or to another interface.

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

// wrap breaks text into lines of at most width characters. It breaks at a
// space, and a word longer than the width takes its own line and goes past it,
// because a path has no space to break at. An empty line of the prose stays,
// because the person wrote it.
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

// ago returns how long before now the time was, in the shortest form that a
// person reads at a glance. The zero time is no time, and gives the empty
// string.
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

// tilde puts a tilde in place of the home of the person, as a shell writes it.
// The rest of the path stays: a person gives a path to another command, and a
// path with a piece missing is one that no command takes.
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

// labelWidth holds the widest label, which is `depends on`, so each value
// starts at one column. Every other label is one word and leaves two columns
// spare; the row of the links keeps its preposition because it names a relation
// and the note of the inbox row names it the same way.
const labelWidth = 10

// ruleWidth is the width of the line below the title.
const ruleWidth = 67

// wrapWidth is the width of the text of flags and of result. It comes from the
// width of the rule, so the text of a field stops where the rule stops and no
// line goes past it. The prose takes no wrap, because the person chose its line
// breaks.
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

// commitText returns the short hash and the subject for the commit row. A hash
// git cannot find gives the hash alone, because only the repository moved.
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

// shown is one ticket with the values that dg show gathers beside it: the file
// that holds the prose, the prose itself, the worktree if it is on disk, the
// ticket that each link of this one names, and the start of the last run. The
// start is the zero time for a ticket that no supervisor has claimed, and the
// worktree is the empty string for one whose worktree is not on disk.
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
	// Each status names how long ago the ticket entered it, which is the time of
	// the last change of its state. A running ticket names how long its run has
	// been going instead, which is the clock the inbox gives on the same run.
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

	// The time takes the line below the status and ends where the status ends.
	// Beside the status it shared the line with the title, and a title of the
	// length a person writes then pushed the pair past the rule. A ticket with
	// no time takes no line, because an empty line above the rule reads as a
	// value that failed to arrive.
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

	// Each link, and not only the ones that still hold the ticket back. The
	// row of the inbox names the tickets that are not done, because that is
	// what the queue acts on; here the person is reading the one ticket and
	// asking what they linked it to.
	if len(s.DependsOn) > 0 {
		writeField(out, "depends on", ticketNames(s.DependsOn))
	}
	if len(s.Blocks) > 0 {
		writeField(out, "blocks", ticketNames(s.Blocks))
	}
}

// writeProse writes the prose as the person wrote it. It is markdown, and the
// person chose each line break: a re-wrap breaks a list, and it makes each long
// line into one long line and one short one.
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

// ticketJSON is one ticket for a reader that is not a person. Each key is the
// name that the text form gives the field, and §7 gives the same names to the
// columns, so a reader of the document knows each key without a second table.
// A field with no value is null, so a script tests one thing and not two.
type ticketJSON struct {
	ID       int64      `json:"id"`
	Title    string     `json:"title"`
	Status   string     `json:"status"`
	Project  string     `json:"project"`
	Ticket   string     `json:"ticket"`
	Worktree *string    `json:"worktree"`
	Branch   *string    `json:"branch"`
	Session  *string    `json:"session"`
	Commit   *string    `json:"commit"`
	Created  *time.Time `json:"created"`
	Accepted *time.Time `json:"accepted"`
	Prose    *string    `json:"prose"`

	shown shown
}

// nullable gives the value, and nothing for the empty string.
func nullable(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// nullableTime gives the time, and nothing for the zero time, which is the
// time of a change that the ticket has not had.
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
		ID:       s.ID,
		Title:    s.Title,
		Status:   string(s.Status),
		Project:  s.Project.Path,
		Ticket:   s.ProseFile,
		Worktree: nullable(s.Worktree),
		Branch:   nullable(s.Branch),
		Session:  nullable(s.Session),
		Commit:   nullable(s.Commit),
		Created:  nullableTime(s.Created),
		Accepted: nullableTime(s.Accepted),
		Prose:    nullable(strings.TrimRight(s.Prose, "\n")),
		shown:    s,
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

// writeOnly writes the one field a --*-only flag asks for, and nothing else.
// The value goes out as the person wrote it into the next command line: no
// label, no wrap and no tilde, because a shell does not expand a tilde that
// came from a variable. A field with no value writes no line, as the whole
// ticket leaves out the row of a field that has none.
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

// showTicket reads one ticket into the value that dg writes. The fields come
// from the database, and the prose comes from the file, because the person
// owns the prose and an editor opens a file and not a row.
//
// The caller gives the store, because a command opens one and reconciles once,
// whatever else it reads from the database. The data directory comes off the
// store, which is the directory it was opened on, so there is no second value
// that could name another one.
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

// onlyFlag is one --*-only flag. asks is the field of that flag, and field is
// the one field dg show writes, which the five flags share. pflag calls Set in
// the order that the person typed the flags, so the first of them takes the
// field and the rest find it taken.
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

// showCommand returns the command dg show. With no id it shows the head of
// READY, because that is the ticket the person is nearly always reading.
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
	return cmd
}
