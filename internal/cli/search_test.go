package cli

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/testfix"
)

// searchRows runs dg search and returns the lines it wrote.
func searchRows(t *testing.T, dataDir, workDir string, args ...string) []string {
	t.Helper()
	out, err := runIn(t, dataDir, workDir, append([]string{"search"}, args...)...)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(out, "\n"), "\n")
}

func TestSearchFindsATitle(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	if _, err := runIn(t, dataDir, repo, "ticket", "Remove the staging app", "--no-body"); err != nil {
		t.Fatal(err)
	}

	rows := searchRows(t, dataDir, repo, "staging")
	if len(rows) != 1 || !strings.Contains(rows[0], "Remove the staging app") {
		t.Errorf("dg search wrote %q, want the title match", rows)
	}
}

func TestSearchFindsProseWithoutWritingIt(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	if _, err := runIn(t, dataDir, repo, "ticket", "Remove the app", "--no-body"); err != nil {
		t.Fatal(err)
	}
	// The command test helper has no stdin to give --body-file, so write the
	// prose as the editor owns it after dg ticket has made the file.
	if err := os.WriteFile(proseFile(dataDir, 1), []byte("Remove the DNS records.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rows := searchRows(t, dataDir, repo, "DNS")
	if len(rows) != 1 || !strings.Contains(rows[0], "Remove the app") || strings.Contains(rows[0], "DNS") {
		t.Errorf("dg search wrote %q, want the ticket but not its matching prose", rows)
	}
}

func TestSearchWithNoMatchSaysSo(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	if _, err := runIn(t, dataDir, repo, "ticket", "Remove the staging app", "--no-body"); err != nil {
		t.Fatal(err)
	}
	rows := searchRows(t, dataDir, repo, "nothing")
	if got, want := rows, []string{"No tickets match."}; !slices.Equal(got, want) {
		t.Errorf("dg search wrote %q, want %q", got, want)
	}
}

func TestSearchProjectAndCase(t *testing.T) {
	dataDir := t.TempDir()
	here := testfix.Repo(t, repoBranch)
	elsewhere := testfix.Repo(t, "release")
	if _, err := runIn(t, dataDir, here, "ticket", "the local ticket", "--no-body"); err != nil {
		t.Fatal(err)
	}
	if _, err := runIn(t, dataDir, elsewhere, "ticket", "the remote ticket", "--no-body"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(proseFile(dataDir, 1), []byte("Need a FROBNICATOR.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(proseFile(dataDir, 2), []byte("Need a frobnicator too.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if rows := searchRows(t, dataDir, here, "frobnicator"); len(rows) != 2 {
		t.Errorf("dg search wrote %q, want both tickets", rows)
	}
	rows := searchRows(t, dataDir, here, "FROBNICATOR", "--project", elsewhere)
	if len(rows) != 1 || !strings.Contains(rows[0], "remote") {
		t.Errorf("dg search --project wrote %q, want only the other project", rows)
	}
}
