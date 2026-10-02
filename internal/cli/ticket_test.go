package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

// projectPath reads the canonical Git root so tests work when temporary
// directories contain symlinks.
func projectPath(t *testing.T, dir string) string {
	t.Helper()
	root, err := project.Root(dir)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// projectRows returns each project row of a data directory.
func projectRows(t *testing.T, dataDir string) []store.Project {
	t.Helper()
	rows, err := testfix.OpenStore(t, dataDir).Projects()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// idOf returns the id that dg ticket wrote.
func idOf(t *testing.T, out string) int64 {
	t.Helper()
	id, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil {
		t.Fatalf("dg ticket wrote %q, want an id", out)
	}
	return id
}

// twoTickets makes two tickets for a later one to wait for, and returns their
// ids.
func twoTickets(t *testing.T, dataDir, repo string) (int64, int64) {
	t.Helper()
	first, err := ticketIn(t, dataDir, repo, "Remove staging infrastructure", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := ticketIn(t, dataDir, repo, "Add rate limiting", "")
	if err != nil {
		t.Fatal(err)
	}
	return first, second
}

func TestRunTicketShowsTheIDOfTheNewTicket(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)

	out, err := runIn(t, dataDir, repo, "ticket", "Remove staging infrastructure", "--no-body")
	if err != nil {
		t.Fatal(err)
	}
	if out != "1\n" {
		t.Errorf("the command wrote %q, want %q", out, "1\n")
	}

	// the second ticket shows the next id
	out, err = runIn(t, dataDir, repo, "ticket", "Add rate limiting", "--no-body")
	if err != nil {
		t.Fatal(err)
	}
	if out != "2\n" {
		t.Errorf("the command wrote %q, want %q", out, "2\n")
	}
}

func TestRunTicketWithNoArgumentsOpensTheEditor(t *testing.T) {
	dataDir := t.TempDir()
	withEditor(t, "Remove staging infrastructure\n\nRemove the staging app.\n")

	out, err := runIn(t, dataDir, testfix.Repo(t, repoBranch), "ticket")
	if err != nil {
		t.Fatal(err)
	}
	if out != "1\n" {
		t.Errorf("the command wrote %q, want %q", out, "1\n")
	}

	// Use a distinctive title to prove it came from the editor.
	queue, err := testfix.OpenStore(t, dataDir).ListQueue()
	if err != nil {
		t.Fatal(err)
	}
	if want := "Remove staging infrastructure"; queue[0].Title != want {
		t.Errorf("title = %q, want %q", queue[0].Title, want)
	}
}

func TestRunTicketWithAProjectUsesThatRepository(t *testing.T) {
	dataDir := t.TempDir()
	here := testfix.Repo(t, repoBranch)
	elsewhere := testfix.Repo(t, "release")

	out, err := runIn(t, dataDir, here, "ticket", "--project", elsewhere, "Remove staging infrastructure", "--no-body")
	if err != nil {
		t.Fatal(err)
	}
	if out != "1\n" {
		t.Errorf("the command wrote %q, want %q", out, "1\n")
	}

	rows := projectRows(t, dataDir)
	if len(rows) != 1 {
		t.Fatalf("the database holds %d projects, want 1", len(rows))
	}
	if want := projectPath(t, elsewhere); rows[0].Path != want {
		t.Errorf("path = %q, want %q", rows[0].Path, want)
	}
}

func TestRunTicketWithARelativeProject(t *testing.T) {
	dataDir := t.TempDir()
	here := testfix.Repo(t, repoBranch)
	elsewhere := testfix.Repo(t, "release")
	relative, err := filepath.Rel(here, elsewhere)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := runIn(t, dataDir, here, "ticket", "--project", relative, "Remove staging infrastructure", "--no-body"); err != nil {
		t.Fatal(err)
	}

	rows := projectRows(t, dataDir)
	if want := projectPath(t, elsewhere); len(rows) != 1 || rows[0].Path != want {
		t.Errorf("the projects are %v, want the one at %q", rows, want)
	}
}

// linkedWorktree makes the primary checkout and one linked worktree that git
// records for it.
func linkedWorktree(t *testing.T) (string, string) {
	t.Helper()
	primary := testfix.Repo(t, repoBranch)
	testfix.CommitIn(t, primary, "first")
	linked := filepath.Join(t.TempDir(), "linked")
	testfix.GitIn(t, primary, "worktree", "add", "-b", "linked", linked)
	t.Cleanup(func() { testfix.GitIn(t, primary, "worktree", "remove", "--force", linked) })
	return primary, linked
}

func TestRunTicketInALinkedWorktreeUsesThePrimaryCheckout(t *testing.T) {
	dataDir := t.TempDir()
	primary, linked := linkedWorktree(t)

	out, err := runIn(t, dataDir, linked, "ticket", "Remove staging infrastructure", "--no-body")
	if err != nil {
		t.Fatal(err)
	}
	if out != "1\n" {
		t.Errorf("the command wrote %q, want %q", out, "1\n")
	}
	if rows := projectRows(t, dataDir); len(rows) != 1 || rows[0].Path != primary {
		t.Errorf("projects = %v, want the primary checkout %q", rows, primary)
	}
}

func TestRunTicketWithALinkedWorktreeProjectUsesThePrimaryCheckout(t *testing.T) {
	dataDir := t.TempDir()
	primary, linked := linkedWorktree(t)

	out, err := runIn(t, dataDir, primary, "ticket", "--project", linked, "Remove staging infrastructure", "--no-body")
	if err != nil {
		t.Fatal(err)
	}
	if out != "1\n" {
		t.Errorf("the command wrote %q, want %q", out, "1\n")
	}
	if rows := projectRows(t, dataDir); len(rows) != 1 || rows[0].Path != primary {
		t.Errorf("projects = %v, want the primary checkout %q", rows, primary)
	}
}

func TestRunTicketWithThePrimaryProjectFromALinkedWorktree(t *testing.T) {
	dataDir := t.TempDir()
	primary, linked := linkedWorktree(t)

	out, err := runIn(t, dataDir, linked, "ticket", "--project", primary, "Remove staging infrastructure", "--no-body")
	if err != nil {
		t.Fatal(err)
	}
	if out != "1\n" {
		t.Errorf("the command wrote %q, want %q", out, "1\\n")
	}
	rows := projectRows(t, dataDir)
	if len(rows) != 1 || rows[0].Path != primary {
		t.Errorf("projects = %v, want the primary checkout %q", rows, primary)
	}
}

func TestRunTicketWithAProjectThatIsNotThere(t *testing.T) {
	dataDir := t.TempDir()
	missing := filepath.Join(t.TempDir(), "nowhere")

	out, err := runIn(t, dataDir, testfix.Repo(t, repoBranch), "ticket", "--project", missing, "Remove staging infrastructure", "--no-body")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want a path that is not there", err)
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("the error is %q, and does not name %q", err, missing)
	}
	if out != "" {
		t.Errorf("the command wrote %q, want nothing", out)
	}
	if files := proseFiles(t, dataDir); len(files) != 0 {
		t.Errorf("the files of prose are %v, want none", files)
	}
}

func TestRunTicketWithAProjectThatIsAFile(t *testing.T) {
	dataDir := t.TempDir()
	file := filepath.Join(t.TempDir(), "ticket.md")
	if err := os.WriteFile(file, nil, filePerm); err != nil {
		t.Fatal(err)
	}

	_, err := runIn(t, dataDir, testfix.Repo(t, repoBranch), "ticket", "--project", file, "Remove staging infrastructure", "--no-body")
	if err == nil {
		t.Fatal("the command gave no error")
	}
	if want := file + " is not a directory"; err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}
}

func TestTicketWritesTheRowTheProseAndTheQueue(t *testing.T) {
	dataDir := t.TempDir()
	const title = "Remove staging infrastructure"

	id, err := ticketIn(t, dataDir, testfix.Repo(t, repoBranch), title, "")
	if err != nil {
		t.Fatal(err)
	}

	queue, err := testfix.OpenStore(t, dataDir).ListQueue()
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 1 {
		t.Fatalf("the queue holds %d tickets, want 1", len(queue))
	}
	if queue[0].ID != id {
		t.Errorf("the queue holds ticket %d, want %d", queue[0].ID, id)
	}
	if queue[0].Title != title {
		t.Errorf("title = %q, want %q", queue[0].Title, title)
	}

	if got := proseFiles(t, dataDir); len(got) != 1 {
		t.Errorf("the files of prose are %v, want one", got)
	}
}

// Reject non-repositories before allocating a ticket ID.
func TestTicketOutsideARepositoryUsesNoID(t *testing.T) {
	dataDir := t.TempDir()

	_, err := ticketIn(t, dataDir, t.TempDir(), "Remove staging infrastructure", "")
	if !errors.Is(err, project.ErrNotARepository) {
		t.Fatalf("err = %v, want %v", err, project.ErrNotARepository)
	}

	id, err := ticketIn(t, dataDir, testfix.Repo(t, repoBranch), "Add rate limiting", "")
	if err != nil {
		t.Fatal(err)
	}
	if id != 1 {
		t.Errorf("id = %d, want 1", id)
	}
}

// The first ticket registers the project's default branch.
func TestTicketWritesTheBranchOfTheRepository(t *testing.T) {
	dataDir := t.TempDir()

	if _, err := ticketIn(t, dataDir, testfix.Repo(t, repoBranch), "Remove staging infrastructure", ""); err != nil {
		t.Fatal(err)
	}

	projects := projectRows(t, dataDir)
	if len(projects) != 1 {
		t.Fatalf("the database holds %d projects, want 1", len(projects))
	}
	if projects[0].DefaultBranch != repoBranch {
		t.Errorf("default branch = %q, want %q", projects[0].DefaultBranch, repoBranch)
	}
}

func TestTicketUsesTheProjectOfAnEarlierTicket(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)

	if _, err := ticketIn(t, dataDir, repo, "Remove staging infrastructure", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := ticketIn(t, dataDir, repo, "Add rate limiting", ""); err != nil {
		t.Fatal(err)
	}

	projects := projectRows(t, dataDir)
	if len(projects) != 1 {
		t.Errorf("the database holds %d projects, want 1", len(projects))
	}
}

func TestTicketWritesTheBodyIntoTheProse(t *testing.T) {
	dataDir := t.TempDir()
	const body = "Remove the staging app, the volume and the records of the DNS."

	if _, err := ticketIn(t, dataDir, testfix.Repo(t, repoBranch), "Remove staging infrastructure", body); err != nil {
		t.Fatal(err)
	}

	if got := proseOf(t, dataDir); got != body {
		t.Errorf("the prose is %q, want %q", got, body)
	}
}

func TestTicketWithNoBodyLeavesTheProseEmpty(t *testing.T) {
	dataDir := t.TempDir()

	if _, err := ticketIn(t, dataDir, testfix.Repo(t, repoBranch), "Remove staging infrastructure", ""); err != nil {
		t.Fatal(err)
	}

	if got := proseOf(t, dataDir); got != "" {
		t.Errorf("the prose is %q, want it empty", got)
	}
}

func TestTicketWritesTheProseForItsPersonOnly(t *testing.T) {
	dataDir := t.TempDir()

	if _, err := ticketIn(t, dataDir, testfix.Repo(t, repoBranch), "Remove staging infrastructure", "body"); err != nil {
		t.Fatal(err)
	}

	files := proseFiles(t, dataDir)
	if len(files) != 1 {
		t.Fatalf("the files of prose are %v, want one", files)
	}
	info, err := os.Stat(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("the permission is %v, want %v", got, os.FileMode(0o600))
	}
}

// Editor and argument input must use the same empty-title validation.
func TestTicketWithNoTitle(t *testing.T) {
	dataDir := t.TempDir()

	_, err := ticketIn(t, dataDir, testfix.Repo(t, repoBranch), "   ", "Remove the staging app.")
	if !errors.Is(err, ErrNoTitle) {
		t.Fatalf("err = %v, want ErrNoTitle", err)
	}

	queue, err := testfix.OpenStore(t, dataDir).ListQueue()
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 0 {
		t.Errorf("the queue holds %d tickets, want none", len(queue))
	}
}

// Ticket creation triggers scheduling; the supervisor selects its own ticket.
func TestTicketStartsARunWhenNothingIsRunning(t *testing.T) {
	dataDir := testfix.XDGDataDir(t)
	repo := testfix.Repo(t, repoBranch)
	l, record := testfix.RecordingLaunch(t)
	useLaunch(t, l)

	if _, err := runIn(t, dataDir, repo, "ticket", "Add the thing", "--no-body"); err != nil {
		t.Fatal(err)
	}

	testfix.WaitForStarts(t, record, 1)
}

func TestRunTicketAfterTwoTickets(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	first, second := twoTickets(t, dataDir, repo)

	out, err := runIn(t, dataDir, repo,
		"ticket", "--after", fmt.Sprint(first), "--after", fmt.Sprint(second), "Remove the last of it", "--no-body")
	if err != nil {
		t.Fatal(err)
	}
	id := idOf(t, out)
	if got, want := testfix.Dependencies(t, dataDir, id), []int64{first, second}; !slices.Equal(got, want) {
		t.Errorf("ticket %d depends on %v, want %v", id, got, want)
	}
}

// Accept comma-separated dependencies as well as repeated flags.
func TestRunTicketAfterACommaList(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	first, second := twoTickets(t, dataDir, repo)

	out, err := runIn(t, dataDir, repo,
		"ticket", "--after", fmt.Sprintf("%d,%d", first, second), "Remove the last of it", "--no-body")
	if err != nil {
		t.Fatal(err)
	}
	id := idOf(t, out)
	if got, want := testfix.Dependencies(t, dataDir, id), []int64{first, second}; !slices.Equal(got, want) {
		t.Errorf("ticket %d depends on %v, want %v", id, got, want)
	}
}

func TestRunTicketAfterATicketWithTheEditor(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	first, _ := twoTickets(t, dataDir, repo)
	withEditor(t, "Remove the last of it\n\nAnd the app with it.\n")

	out, err := runIn(t, dataDir, repo, "ticket", "--after", fmt.Sprint(first), "--no-body")
	if err != nil {
		t.Fatal(err)
	}
	id := idOf(t, out)
	if got, want := testfix.Dependencies(t, dataDir, id), []int64{first}; !slices.Equal(got, want) {
		t.Errorf("ticket %d depends on %v, want %v", id, got, want)
	}
}

func TestRunTicketAfterSomethingThatIsNotANumber(t *testing.T) {
	dataDir := t.TempDir()

	_, err := runIn(t, dataDir, testfix.Repo(t, repoBranch), "ticket", "--after", "twelve", "Remove the last of it", "--no-body")
	if err == nil {
		t.Fatal("the command gave no error")
	}
	if !strings.Contains(err.Error(), "twelve") {
		t.Errorf("the error is %q, and does not name %q", err, "twelve")
	}
}

// A missing dependency must roll back both ticket creation and edges.
func TestRunTicketAfterATicketThatIsNotThere(t *testing.T) {
	dataDir := t.TempDir()
	const missing = 12

	out, err := runIn(t, dataDir, testfix.Repo(t, repoBranch),
		"ticket", "--after", fmt.Sprint(missing), "Remove the last of it", "--no-body")
	if !errors.Is(err, store.ErrNoTicket) {
		t.Fatalf("err = %v, want ErrNoTicket", err)
	}
	if !strings.Contains(err.Error(), fmt.Sprint(missing)) {
		t.Errorf("the error is %q, and does not name %d", err, missing)
	}
	if out != "" {
		t.Errorf("the command wrote %q, want nothing", out)
	}

	queue, err := testfix.OpenStore(t, dataDir).ListQueue()
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 0 {
		t.Errorf("the queue holds %d tickets, want none", len(queue))
	}
	if files := proseFiles(t, dataDir); len(files) != 0 {
		t.Errorf("the files of prose are %v, want none", files)
	}
}

// File input supports descriptions beyond command-line size limits.
func TestRunTicketWithABodyFileReadsTheProseFromTheFile(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	const title = "Remove staging infrastructure"
	const body = "Remove the staging app, the volume and the records of the DNS.\n"
	path := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(path, []byte(body), filePerm); err != nil {
		t.Fatal(err)
	}

	fromArgument, err := runIn(t, dataDir, repo, "ticket", title, body)
	if err != nil {
		t.Fatal(err)
	}
	fromFile, err := runIn(t, dataDir, repo, "ticket", title, "--body-file", path)
	if err != nil {
		t.Fatal(err)
	}

	argumentID, fileID := idOf(t, fromArgument), idOf(t, fromFile)
	if got := proseOfTicket(t, dataDir, fileID); got != body {
		t.Errorf("the prose is %q, want %q", got, body)
	}
	s := testfix.OpenStore(t, dataDir)
	one, err := s.Ticket(argumentID)
	if err != nil {
		t.Fatal(err)
	}
	two, err := s.Ticket(fileID)
	if err != nil {
		t.Fatal(err)
	}
	if one.Title != two.Title {
		t.Errorf("the titles are %q and %q, want the same", one.Title, two.Title)
	}
	if one.Project != two.Project {
		t.Errorf("the projects are %v and %v, want the same", one.Project, two.Project)
	}
	if got := proseOfTicket(t, dataDir, argumentID); got != proseOfTicket(t, dataDir, fileID) {
		t.Errorf("the prose of the body argument is %q and the prose of the file is %q", got, proseOfTicket(t, dataDir, fileID))
	}
}

func TestRunTicketWithABodyFileOfADashReadsTheStandardInput(t *testing.T) {
	dataDir := t.TempDir()
	const body = "Remove the staging app, the volume and the records of the DNS.\n"

	out, err := runInWithStdin(t, dataDir, testfix.Repo(t, repoBranch), body,
		"ticket", "Remove staging infrastructure", "--body-file", "-")
	if err != nil {
		t.Fatal(err)
	}

	if got := proseOfTicket(t, dataDir, idOf(t, out)); got != body {
		t.Errorf("the prose is %q, want %q", got, body)
	}
}

func TestRunTicketWithABodyFileAndABodyArgument(t *testing.T) {
	dataDir := t.TempDir()
	path := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(path, []byte("From the file.\n"), filePerm); err != nil {
		t.Fatal(err)
	}

	out, err := runIn(t, dataDir, testfix.Repo(t, repoBranch),
		"ticket", "Remove staging infrastructure", "From the argument.", "--body-file", path)
	if err == nil {
		t.Fatal("the command gave no error")
	}
	if want := "dg ticket takes the prose from a body or from --body-file, and got both"; err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}
	if out != "" {
		t.Errorf("the command wrote %q, want nothing", out)
	}
	if files := proseFiles(t, dataDir); len(files) != 0 {
		t.Errorf("the files of prose are %v, want none", files)
	}
}

func TestRunTicketWithABodyFileThatIsNotThere(t *testing.T) {
	dataDir := t.TempDir()
	missing := filepath.Join(t.TempDir(), "body.md")

	out, err := runIn(t, dataDir, testfix.Repo(t, repoBranch),
		"ticket", "Remove staging infrastructure", "--body-file", missing)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want a path that is not there", err)
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("the error is %q, and does not name %q", err, missing)
	}
	if out != "" {
		t.Errorf("the command wrote %q, want nothing", out)
	}
	if files := proseFiles(t, dataDir); len(files) != 0 {
		t.Errorf("the files of prose are %v, want none", files)
	}

	queue, err := testfix.OpenStore(t, dataDir).ListQueue()
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 0 {
		t.Errorf("the queue holds %d tickets, want none", len(queue))
	}
}

// A body file without a title must fail rather than open an editor and
// discard the file input.
func TestRunTicketWithABodyFileAndNoTitle(t *testing.T) {
	dataDir := t.TempDir()
	path := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(path, []byte("Remove the staging app.\n"), filePerm); err != nil {
		t.Fatal(err)
	}

	_, err := runIn(t, dataDir, testfix.Repo(t, repoBranch), "ticket", "--body-file", path)
	if !errors.Is(err, ErrNoTitle) {
		t.Fatalf("err = %v, want ErrNoTitle", err)
	}
	if files := proseFiles(t, dataDir); len(files) != 0 {
		t.Errorf("the files of prose are %v, want none", files)
	}
}

// Regression: dg ticket list once created ticket 159 titled "list". Title-
// only command lines now require an explicit --no-body.
func TestRunTicketWithNoBodyAndNoFlag(t *testing.T) {
	dataDir := t.TempDir()

	out, err := runIn(t, dataDir, testfix.Repo(t, repoBranch), "ticket", "list")
	if err == nil {
		t.Fatal("the command gave no error")
	}
	for _, want := range []string{"no body", "--no-body"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error is %q, and does not say %q", err, want)
		}
	}
	if out != "" {
		t.Errorf("the command wrote %q, want nothing", out)
	}
	if files := proseFiles(t, dataDir); len(files) != 0 {
		t.Errorf("the files of prose are %v, want none", files)
	}
	queue, err := testfix.OpenStore(t, dataDir).ListQueue()
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 0 {
		t.Errorf("the queue holds %d tickets, want none", len(queue))
	}
}

// No description must also mean no empty description section in dg show.
func TestRunTicketWithNoBodyAddsTheTicket(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)

	out, err := runIn(t, dataDir, repo, "ticket", "Remove staging infrastructure", "--no-body")
	if err != nil {
		t.Fatal(err)
	}
	id := idOf(t, out)
	if got := proseOfTicket(t, dataDir, id); got != "" {
		t.Errorf("the prose is %q, want nothing", got)
	}

	shown, err := runIn(t, dataDir, repo, "show", fmt.Sprint(id))
	if err != nil {
		t.Fatal(err)
	}
	_, fields, ok := strings.Cut(shown, "─\n")
	if !ok {
		t.Fatalf("dg show wrote no rule under the title:\n%s", shown)
	}
	if strings.Contains(fields, "\n\n") {
		t.Errorf("dg show holds a prose section for a ticket with no body:\n%s", shown)
	}
}

func TestRunTicketWithABodyArgumentAndNoBody(t *testing.T) {
	dataDir := t.TempDir()

	out, err := runIn(t, dataDir, testfix.Repo(t, repoBranch),
		"ticket", "Remove staging infrastructure", "Remove the staging app.", "--no-body")
	if err == nil {
		t.Fatal("the command gave no error")
	}
	if want := "dg ticket takes a body or --no-body, and got both"; err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}
	if out != "" {
		t.Errorf("the command wrote %q, want nothing", out)
	}
	if files := proseFiles(t, dataDir); len(files) != 0 {
		t.Errorf("the files of prose are %v, want none", files)
	}
}

func TestRunTicketWithABodyFileAndNoBody(t *testing.T) {
	dataDir := t.TempDir()
	path := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(path, []byte("From the file.\n"), filePerm); err != nil {
		t.Fatal(err)
	}

	out, err := runIn(t, dataDir, testfix.Repo(t, repoBranch),
		"ticket", "Remove staging infrastructure", "--body-file", path, "--no-body")
	if err == nil {
		t.Fatal("the command gave no error")
	}
	if want := "dg ticket takes a body or --no-body, and got both"; err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}
	if out != "" {
		t.Errorf("the command wrote %q, want nothing", out)
	}
	if files := proseFiles(t, dataDir); len(files) != 0 {
		t.Errorf("the files of prose are %v, want none", files)
	}
}

// Editor-created title-only tickets remain valid because the user explicitly
// reviewed their contents.
func TestRunTicketFromTheEditorNeedsNoNoBody(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	withEditor(t, "Remove staging infrastructure\n")

	out, err := runIn(t, dataDir, repo, "ticket")
	if err != nil {
		t.Fatal(err)
	}
	id := idOf(t, out)
	if got := proseOfTicket(t, dataDir, id); got != "" {
		t.Errorf("the prose is %q, want nothing", got)
	}

	queue, err := testfix.OpenStore(t, dataDir).ListQueue()
	if err != nil {
		t.Fatal(err)
	}
	if want := "Remove staging infrastructure"; len(queue) != 1 || queue[0].Title != want {
		t.Errorf("the queue is %v, want the one ticket %q", queue, want)
	}
}

func TestTicketHelpNamesNoBody(t *testing.T) {
	out, err := runIn(t, t.TempDir(), testfix.Repo(t, repoBranch), "ticket", "--help")
	if err != nil {
		t.Fatal(err)
	}
	var line string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "--no-body") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("the help of dg ticket names no --no-body:\n%s", out)
	}
	if !strings.Contains(line, "no prose") {
		t.Errorf("the help line of --no-body is %q, and does not say that the ticket has no prose", line)
	}
}
