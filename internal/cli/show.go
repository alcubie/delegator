// The text of one ticket for a terminal. dg show gives a person the fields that
// a run wrote, the four variables that connect the ticket to its work, and the
// prose that the person wrote.

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

// labelWidth holds the widest label of the two reports and the four variables,
// so each value starts at one column.
const labelWidth = 8

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

// writeTicket writes one ticket in full
func writeTicket(out io.Writer, dataDir string, t store.Ticket, prose string, now time.Time) {
	// Only a ready ticket has a time that the heading can name. dg finish
	// writes completed, and no command takes it away, so a ticket that dg
	// revise put back in the queue still holds the time that its earlier run
	// stopped. A time beside queued or running would read as the time that the
	// ticket entered that state, and it is not. Ticket 7 keeps the history of
	// each change of state, and each status takes a time from it.
	var when time.Time
	if t.Status == store.Ready {
		when = t.Completed
	}
	heading := fmt.Sprintf("  #%d  %s", t.ID, t.Title)
	right := string(t.Status)
	if since := ago(when, now); since != "" {
		right += " · " + since
	}
	pad := max(1, ruleWidth-len(heading)-len(right))
	fmt.Fprintf(out, "%s%s%s\n", heading, strings.Repeat(" ", pad), right)
	fmt.Fprintf(out, "  %s\n", strings.Repeat("─", ruleWidth-2))

	if t.Commit != "" {
		writeField(out, "commit", commitText(t.Project.Path, t.Commit))
		fmt.Fprintln(out)
	}

	home, _ := os.UserHomeDir()
	writeField(out, "project", tilde(t.Project.Path, home))
	writeField(out, "ticket", tilde(proseFile(dataDir, t.ID), home))
	writeField(out, "worktree", tilde(run.WorktreePath(dataDir, t.ID), home))
	if t.Branch != "" {
		writeField(out, "branch", t.Branch)
	}
	if t.Session != "" {
		writeField(out, "session", t.Session)
	}

	// The prose goes out as the person wrote it. It is markdown, and the person
	// chose each line break: a re-wrap breaks a list, and it makes each long
	// line into one long line and one short one.
	if prose = strings.TrimRight(prose, "\n"); prose != "" {
		fmt.Fprintln(out)
		for _, line := range strings.Split(prose, "\n") {
			fmt.Fprintln(out, strings.TrimRight("  "+line, " "))
		}
	}
}

// showTicket reads one ticket and writes it. The fields come from the database,
// and the prose comes from the file, because the person owns the prose and an
// editor opens a file and not a row.
func showTicket(out io.Writer, dataDir string, id int64) error {
	return store.With(dataDir, func(s *store.Store) error {

		ticket, err := s.Ticket(id)
		if err != nil {
			return err
		}

		// A ticket that has no file of prose yet is not a fault of dg show.
		prose, err := os.ReadFile(proseFile(dataDir, id))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}

		writeTicket(out, dataDir, ticket, string(prose), time.Now().UTC())
		return nil
	})
}

// showCommand returns the command dg show.
func showCommand(dataDir string) *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show the details of a ticket.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("%q is not the id of a ticket", args[0])
			}
			return showTicket(cmd.OutOrStdout(), dataDir, id)
		},
	}
}
