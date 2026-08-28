package cli

import (
	"os"
	"os/exec"
	"strings"
)

// defaultEditor is the editor that delegator starts when the person has set no
// EDITOR. It is the editor that POSIX asks each system to have.
const defaultEditor = "vi"

// editor starts the editor of the person on path and waits for it to stop. It
// is a variable so that a test can put its own editor in place of it.
var editor = startEditor

// editorName returns the editor of the person, or the one that each system has if
// the person has set none.
func editorName() string {
	if name := os.Getenv("EDITOR"); name != "" {
		return name
	}
	return defaultEditor
}

func startEditor(path string) error {
	// EDITOR can hold arguments, as "code --wait" does.
	words := strings.Fields(editorName())
	cmd := exec.Command(words[0], append(words[1:], path)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// splitTitle returns the first line of text and each line below it. The title is
// a column and not the first line of the prose, so the prose does not hold it,
// and the empty lines between the two go away.
func splitTitle(text string) (title, body string) {
	title, body, _ = strings.Cut(text, "\n")
	return strings.TrimSpace(title), strings.TrimLeft(body, "\n")
}

// TicketFromEditor makes a ticket from what the person writes in an editor. The
// first line is the title, and each line below it is the prose.
func TicketFromEditor(dataDir, workDir string) (int64, error) {
	f, err := os.CreateTemp("", "dg-*.md")
	if err != nil {
		return 0, err
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)

	if err := editor(path); err != nil {
		return 0, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	title, body := splitTitle(string(data))
	return Ticket(dataDir, workDir, title, body)
}
