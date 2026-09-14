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

// projectPath returns the path that a ticket made in dir writes to its project.
// git gives it, and a test that built the path itself would pass on a machine
// whose temporary directory is a symbolic link and fail on one where it is not.
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

	out, err := runIn(t, dataDir, repo, "ticket", "Remove staging infrastructure")
	if err != nil {
		t.Fatal(err)
	}
	if out != "1\n" {
		t.Errorf("the command wrote %q, want %q", out, "1\n")
	}

	// the second ticket shows the next id
	out, err = runIn(t, dataDir, repo, "ticket", "Add rate limiting")
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

	// The title comes from the editor, and not from an argument that is not
	// there, so the title says that the editor gave it.
	queue, err := testfix.OpenStore(t, dataDir).ListQueue()
	if err != nil {
		t.Fatal(err)
	}
	if want := "Remove staging infrastructure"; queue[0].Title != want {
		t.Errorf("title = %q, want %q", queue[0].Title, want)
	}
}

// The flag --project names the repository the ticket is for, so a person or an
// agent in another directory can file a ticket without leaving it.
func TestRunTicketWithAProjectUsesThatRepository(t *testing.T) {
	dataDir := t.TempDir()
	here := testfix.Repo(t, repoBranch)
	elsewhere := testfix.Repo(t, "release")

	out, err := runIn(t, dataDir, here, "ticket", "--project", elsewhere, "Remove staging infrastructure")
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

// A path that is not absolute starts at the directory dg runs in, which is what
// a person who typed it at a shell meant.
func TestRunTicketWithARelativeProject(t *testing.T) {
	dataDir := t.TempDir()
	here := testfix.Repo(t, repoBranch)
	elsewhere := testfix.Repo(t, "release")
	relative, err := filepath.Rel(here, elsewhere)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := runIn(t, dataDir, here, "ticket", "--project", relative, "Remove staging infrastructure"); err != nil {
		t.Fatal(err)
	}

	rows := projectRows(t, dataDir)
	if want := projectPath(t, elsewhere); len(rows) != 1 || rows[0].Path != want {
		t.Errorf("the projects are %v, want the one at %q", rows, want)
	}
}

// A --project that names nothing costs no ticket, and the error is the path and
// not a sentence about git.
func TestRunTicketWithAProjectThatIsNotThere(t *testing.T) {
	dataDir := t.TempDir()
	missing := filepath.Join(t.TempDir(), "nowhere")

	out, err := runIn(t, dataDir, testfix.Repo(t, repoBranch), "ticket", "--project", missing, "Remove staging infrastructure")
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

// A --project that names a file is not a project either.
func TestRunTicketWithAProjectThatIsAFile(t *testing.T) {
	dataDir := t.TempDir()
	file := filepath.Join(t.TempDir(), "ticket.md")
	if err := os.WriteFile(file, nil, filePerm); err != nil {
		t.Fatal(err)
	}

	_, err := runIn(t, dataDir, testfix.Repo(t, repoBranch), "ticket", "--project", file, "Remove staging infrastructure")
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

// Ticket finds the project before it writes a row, so a directory that is not a
// repository costs no id.
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

// The branch of the project is the start of the branch of each run, so the
// first ticket of a project settles it.
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

// A second ticket in the same repository uses the project that the first one
// made, because the path of a project is unique.
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

// A ticket can hold private data, so only the person who made it can read the
// prose.
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

// The title comes from an argument here, and a ticket with no title is the same
// error as one that comes from the editor.
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

// A person adds a ticket and walks away, so dg ticket is the command that
// starts the queue moving. It starts a supervisor and names no ticket: the
// supervisor claims the first ticket of the queue for itself.
func TestTicketStartsARunWhenNothingIsRunning(t *testing.T) {
	dataDir := testfix.XDGDataDir(t)
	repo := testfix.Repo(t, repoBranch)
	l, marker := testfix.RecordingLaunch(t)
	useLaunch(t, l)

	if _, err := runIn(t, dataDir, repo, "ticket", "Add the thing"); err != nil {
		t.Fatal(err)
	}

	testfix.WaitForStarts(t, marker, 1)
}

// The flag repeats, which is the form the help text gives.
func TestRunTicketAfterTwoTickets(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	first, second := twoTickets(t, dataDir, repo)

	out, err := runIn(t, dataDir, repo,
		"ticket", "--after", fmt.Sprint(first), "--after", fmt.Sprint(second), "Remove the last of it")
	if err != nil {
		t.Fatal(err)
	}
	id := idOf(t, out)
	if got, want := testfix.Dependencies(t, dataDir, id), []int64{first, second}; !slices.Equal(got, want) {
		t.Errorf("ticket %d depends on %v, want %v", id, got, want)
	}
}

// cobra's slice flag also takes the ids in one comma list, and a person who
// writes that means the same thing as a person who repeats the flag.
func TestRunTicketAfterACommaList(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	first, second := twoTickets(t, dataDir, repo)

	out, err := runIn(t, dataDir, repo,
		"ticket", "--after", fmt.Sprintf("%d,%d", first, second), "Remove the last of it")
	if err != nil {
		t.Fatal(err)
	}
	id := idOf(t, out)
	if got, want := testfix.Dependencies(t, dataDir, id), []int64{first, second}; !slices.Equal(got, want) {
		t.Errorf("ticket %d depends on %v, want %v", id, got, want)
	}
}

// The flag works with the editor form as well as with the title and the body.
func TestRunTicketAfterATicketWithTheEditor(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	first, _ := twoTickets(t, dataDir, repo)
	withEditor(t, "Remove the last of it\n\nAnd the app with it.\n")

	out, err := runIn(t, dataDir, repo, "ticket", "--after", fmt.Sprint(first))
	if err != nil {
		t.Fatal(err)
	}
	id := idOf(t, out)
	if got, want := testfix.Dependencies(t, dataDir, id), []int64{first}; !slices.Equal(got, want) {
		t.Errorf("ticket %d depends on %v, want %v", id, got, want)
	}
}

// An --after that is not a number is cobra's error, and it names what the
// person typed.
func TestRunTicketAfterSomethingThatIsNotANumber(t *testing.T) {
	dataDir := t.TempDir()

	_, err := runIn(t, dataDir, testfix.Repo(t, repoBranch), "ticket", "--after", "twelve", "Remove the last of it")
	if err == nil {
		t.Fatal("the command gave no error")
	}
	if !strings.Contains(err.Error(), "twelve") {
		t.Errorf("the error is %q, and does not name %q", err, "twelve")
	}
}

// An --after that names no ticket costs no ticket: the store writes the row
// and the links under one transaction, so the refusal leaves neither.
func TestRunTicketAfterATicketThatIsNotThere(t *testing.T) {
	dataDir := t.TempDir()
	const missing = 12

	out, err := runIn(t, dataDir, testfix.Repo(t, repoBranch),
		"ticket", "--after", fmt.Sprint(missing), "Remove the last of it")
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

// The prose of a GUI form is in memory and not in an argument, and a long
// prose meets the limit of the command line, so --body-file names a file that
// holds it. The ticket it makes is the ticket that the body argument makes.
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

// A path of - is the standard input, as it is for git commit -F, so a program
// that holds the prose can pipe it in and needs no file.
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

// The prose has one source. A body argument beside --body-file leaves dg to
// choose which one the person meant, so it refuses and writes no ticket.
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

// A --body-file that names nothing is a prose the person wrote and dg cannot
// find, so the error holds the path and there is no ticket to fill in later.
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

// The editor is what dg opens when no argument is there, and it would throw
// away the prose the flag names, so a --body-file with no title is the error of
// a ticket with no title.
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
