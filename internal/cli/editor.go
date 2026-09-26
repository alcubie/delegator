package cli

import (
	"os"
	"os/exec"
	"strings"

	"github.com/alcubie/delegator/internal/store"
)

// defaultEditor is the POSIX editor used when EDITOR is unset.
const defaultEditor = "vi"

// editor opens path and waits. Tests replace it with a controlled editor.
var editor = startEditor

// editorName returns EDITOR or the default editor.
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

// splitTitle separates the first line from the description, trimming title
// whitespace and leading blank lines from the description.
func splitTitle(text string) (title, body string) {
	title, body, _ = strings.Cut(text, "\n")
	return strings.TrimSpace(title), strings.TrimLeft(body, "\n")
}

// fromEditor edits text in a temporary file and returns its contents. The
// temporary file is removed afterward.
func fromEditor(text string) (string, error) {
	f, err := os.CreateTemp("", "dg-*.md")
	if err != nil {
		return "", err
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)

	if err := os.WriteFile(path, []byte(text), filePerm); err != nil {
		return "", err
	}
	if err := editor(path); err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// TicketFromEditor creates a ticket from the editor's first line (title) and
// remaining text (description), with the supplied dependencies.
func TicketFromEditor(s *store.Store, workDir string, dependsOn ...int64) (int64, error) {
	text, err := fromEditor("")
	if err != nil {
		return 0, err
	}
	title, body := splitTitle(text)
	return Ticket(s, workDir, title, body, dependsOn...)
}
