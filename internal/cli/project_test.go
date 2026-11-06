package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

var projectResultFields = []string{"id", "name", "path", "default_branch", "archived"}

// registerProjects adds projects to a data directory in the given order, as
// the first ticket of a repository registers one, and returns them in the
// order of their IDs.
func registerProjects(t *testing.T, dataDir string, paths ...string) []store.Project {
	t.Helper()
	s := testfix.OpenStore(t, dataDir)
	for _, path := range paths {
		if _, err := s.ProjectID(path, repoBranch); err != nil {
			t.Fatal(err)
		}
	}
	projects, err := s.Projects()
	if err != nil {
		t.Fatal(err)
	}
	return projects
}

// registerUnsortedProjects registers projects in an order that is not their
// alphabetical order, so a listing left in the order of registration is
// visible, and returns the paths in the order the listing must write them.
func registerUnsortedProjects(t *testing.T, dataDir string) ([]store.Project, []string) {
	t.Helper()
	projects := registerProjects(t, dataDir, "/srv/zulu", "/srv/alpha", "/srv/mike")
	return projects, []string{"/srv/alpha", "/srv/mike", "/srv/zulu"}
}

// projectListResult reads the array that project.list returns through the
// protocol.
func projectListResult(t *testing.T, dataDir, workDir string) []any {
	t.Helper()
	out, err := rpcIn(t, dataDir, workDir, `{"jsonrpc":"2.0","method":"project.list","params":{},"id":1}`)
	if err != nil {
		t.Fatal(err)
	}
	response := rpcObject(t, out)
	if response["error"] != nil {
		t.Fatalf("project.list response = %#v, want a result", response)
	}
	result, ok := response["result"].([]any)
	if !ok {
		t.Fatalf("project.list result = %#v, want an array", response["result"])
	}
	return result
}

// projectPathResult reads the object that an operation naming a project by its
// directory returns through the protocol.
func projectPathResult(t *testing.T, dataDir, workDir, method, dir string) map[string]any {
	t.Helper()
	request := fmt.Sprintf(`{"jsonrpc":"2.0","method":%q,"params":{"args":[%q]},"id":1}`, method, dir)
	out, err := rpcIn(t, dataDir, workDir, request)
	if err != nil {
		t.Fatal(err)
	}
	response := rpcObject(t, out)
	if response["error"] != nil {
		t.Fatalf("%s response = %#v, want a result", method, response)
	}
	result, ok := response["result"].(map[string]any)
	if !ok {
		t.Fatalf("%s result = %#v, want an object", method, response["result"])
	}
	return result
}

func projectAddResult(t *testing.T, dataDir, workDir, dir string) map[string]any {
	t.Helper()
	return projectPathResult(t, dataDir, workDir, "project.add", dir)
}

func projectArchiveResult(t *testing.T, dataDir, workDir, dir string) map[string]any {
	t.Helper()
	return projectPathResult(t, dataDir, workDir, "project.archive", dir)
}

// archivedPaths returns the path of every project that project.list reports as
// archived.
func archivedPaths(t *testing.T, dataDir string) []string {
	t.Helper()
	var paths []string
	for _, value := range projectListResult(t, dataDir, t.TempDir()) {
		row, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("project row = %#v, want an object", value)
		}
		if row["archived"] == true {
			paths = append(paths, fmt.Sprint(row["path"]))
		}
	}
	return paths
}

// projectIDs maps each registered project to its ID, so a test can assert the
// ID of a row it finds by path.
func projectIDs(projects []store.Project) map[string]int64 {
	ids := map[string]int64{}
	for _, project := range projects {
		ids[project.Path] = project.ID
	}
	return ids
}

func TestProjectListReportsAnInstanceWithNoProjects(t *testing.T) {
	dataDir := t.TempDir()

	out, err := runIn(t, dataDir, t.TempDir(), "project", "list")
	if err != nil {
		t.Fatal(err)
	}
	if want := "No projects are registered.\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestProjectListValueOfAnEmptyInstanceIsAnEmptyArray(t *testing.T) {
	dataDir := t.TempDir()

	if got := projectListResult(t, dataDir, t.TempDir()); len(got) != 0 {
		t.Errorf("project.list result = %#v, want an empty array", got)
	}
	encoded, err := json.Marshal(projectValues(nil))
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != "[]" {
		t.Errorf("encoded value = %s, want []", encoded)
	}
}

func TestProjectListWritesEveryProjectInAlphabeticalOrder(t *testing.T) {
	dataDir := t.TempDir()
	_, want := registerUnsortedProjects(t, dataDir)

	out, err := runIn(t, dataDir, t.TempDir(), "project", "list")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != len(want)+1 || !strings.HasPrefix(lines[0], "NAME") ||
		!strings.Contains(lines[0], "PATH") || !strings.Contains(lines[0], "DEFAULT BRANCH") {
		t.Fatalf("output has no NAME, PATH and DEFAULT BRANCH header:\n%s", out)
	}
	for i, path := range want {
		fields := strings.Fields(lines[i+1])
		if len(fields) != 3 || fields[0] != filepath.Base(path) || fields[1] != path || fields[2] != repoBranch {
			t.Errorf("row %d = %q, want %s, %s and %s", i, lines[i+1], filepath.Base(path), path, repoBranch)
		}
	}
}

func TestProjectListMarksAnArchivedProject(t *testing.T) {
	var out strings.Builder
	writeProjects(&out, []projectJSON{
		{ID: 1, Name: "alpha", Path: "/srv/alpha", DefaultBranch: repoBranch},
		{ID: 2, Name: "zulu", Path: "/srv/zulu", DefaultBranch: repoBranch, Archived: true},
	})

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("output = %q, want a header and two rows", out.String())
	}
	if strings.Contains(lines[1], "archived") {
		t.Errorf("the active row = %q, and it is marked archived", lines[1])
	}
	if !strings.Contains(lines[2], "archived") {
		t.Errorf("the archived row = %q, want it marked archived", lines[2])
	}
}

func TestProjectListValueHoldsEveryFieldInAlphabeticalOrder(t *testing.T) {
	dataDir := t.TempDir()
	projects, want := registerUnsortedProjects(t, dataDir)
	ids := projectIDs(projects)

	result := projectListResult(t, dataDir, t.TempDir())
	if len(result) != len(want) {
		t.Fatalf("project.list result = %#v, want %d projects", result, len(want))
	}
	for i, value := range result {
		row, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("project row = %#v, want an object", value)
		}
		if row["path"] != want[i] || row["id"] != float64(ids[want[i]]) ||
			row["default_branch"] != repoBranch {
			t.Errorf("row %d = %#v, want %s with id %d", i, row, want[i], ids[want[i]])
		}
		if row["name"] != filepath.Base(want[i]) || row["archived"] != false {
			t.Errorf("row %d = %#v, want name %s and archived false", i, row, filepath.Base(want[i]))
		}
	}
}

func TestProjectValueReportsAGivenNameAndAnArchivedProject(t *testing.T) {
	archived := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)

	value := projectValue(store.Project{
		ID: 1, Path: "/srv/alpha", DefaultBranch: repoBranch, Name: "Widget", ArchivedAt: archived,
	})

	if value.Name != "Widget" {
		t.Errorf("name = %q, want the given name Widget", value.Name)
	}
	if !value.Archived {
		t.Errorf("value = %#v, want it archived", value)
	}
}

func TestRPCDiscoverProjectListResultContract(t *testing.T) {
	document := rpcDiscover(t)
	validator := openRPCResultValidator(t, document, "project.list")

	empty := t.TempDir()
	if err := validator.Validate(projectListResult(t, empty, t.TempDir())); err != nil {
		t.Fatalf("empty result does not satisfy the advertised schema: %v", err)
	}

	dataDir := t.TempDir()
	registerUnsortedProjects(t, dataDir)
	result := projectListResult(t, dataDir, t.TempDir())
	if err := validator.Validate(result); err != nil {
		t.Fatalf("project list does not satisfy the advertised schema: %v", err)
	}

	requiredResultFields(t, validator, result, firstResultRow, projectResultFields...)
}

func TestRPCDiscoverPublishesProjectList(t *testing.T) {
	document := rpcDiscover(t)
	method := openRPCMethodNamed(t, document, "project.list")
	if method.Summary == "" || method.Description == "" {
		t.Errorf("project.list method = %#v, want its help text", method)
	}
	for _, param := range method.Params {
		if param.Name == "args" {
			t.Errorf("project.list takes positional arguments: %#v", param)
		}
	}
	for _, advertised := range document.Methods {
		if advertised.Name == "project" {
			t.Error("discovery advertises the project namespace as a method")
		}
	}
}

func TestProjectAddRegistersANewRepositoryAndReturnsItsRecord(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)

	result := projectAddResult(t, dataDir, t.TempDir(), repo)

	rows := projectRows(t, dataDir)
	if len(rows) != 1 {
		t.Fatalf("projects = %v, want one", rows)
	}
	if result["id"] != float64(rows[0].ID) || result["path"] != repo || result["default_branch"] != repoBranch {
		t.Errorf("result = %#v, want id %d, path %q, default_branch %q", result, rows[0].ID, repo, repoBranch)
	}
	if result["name"] != filepath.Base(repo) || result["archived"] != false {
		t.Errorf("result = %#v, want name %q and archived false", result, filepath.Base(repo))
	}
}

func TestProjectAddPrintsThePathAsTextOutput(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)

	out, err := runIn(t, dataDir, t.TempDir(), "project", "add", repo)
	if err != nil {
		t.Fatal(err)
	}
	if want := repo + "\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestProjectAddDefaultsToTheCurrentWorkingDirectory(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)

	out, err := runIn(t, dataDir, repo, "project", "add")
	if err != nil {
		t.Fatal(err)
	}
	if want := repo + "\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestProjectAddIsIdempotentForTheSameRepository(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)

	first := projectAddResult(t, dataDir, t.TempDir(), repo)
	second := projectAddResult(t, dataDir, t.TempDir(), repo)

	if first["id"] != second["id"] {
		t.Errorf("second add id = %#v, want %#v", second["id"], first["id"])
	}
	if rows := projectRows(t, dataDir); len(rows) != 1 {
		t.Errorf("projects = %v, want one", rows)
	}
}

func TestProjectAddFromASubdirectoryResolvesToTheRoot(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	sub := filepath.Join(repo, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	result := projectAddResult(t, dataDir, t.TempDir(), sub)
	if result["path"] != repo {
		t.Errorf("result path = %#v, want the repository root %q", result["path"], repo)
	}
	if rows := projectRows(t, dataDir); len(rows) != 1 || rows[0].Path != repo {
		t.Errorf("projects = %v, want the root %q", rows, repo)
	}
}

func TestProjectAddFromALinkedWorktreeResolvesToThePrimaryCheckout(t *testing.T) {
	dataDir := t.TempDir()
	primary, linked := linkedWorktree(t)

	result := projectAddResult(t, dataDir, t.TempDir(), linked)
	if result["path"] != primary {
		t.Errorf("result path = %#v, want the primary checkout %q", result["path"], primary)
	}
	if rows := projectRows(t, dataDir); len(rows) != 1 || rows[0].Path != primary {
		t.Errorf("projects = %v, want the primary checkout %q", rows, primary)
	}
}

func TestProjectAddRejectsANonRepository(t *testing.T) {
	dataDir := t.TempDir()
	notARepo := t.TempDir()

	_, err := runIn(t, dataDir, t.TempDir(), "project", "add", notARepo)
	if !errors.Is(err, project.ErrNotARepository) {
		t.Fatalf("err = %v, want %v", err, project.ErrNotARepository)
	}
	if rows := projectRows(t, dataDir); len(rows) != 0 {
		t.Errorf("projects = %v, want none", rows)
	}
}

func TestProjectAddRejectsAnEmptyRepository(t *testing.T) {
	dataDir := t.TempDir()
	empty := testfix.EmptyRepo(t, repoBranch)

	_, err := runIn(t, dataDir, t.TempDir(), "project", "add", empty)
	if !errors.Is(err, project.ErrNoCommit) {
		t.Fatalf("err = %v, want %v", err, project.ErrNoCommit)
	}
	if rows := projectRows(t, dataDir); len(rows) != 0 {
		t.Errorf("projects = %v, want none", rows)
	}
}

func TestRPCProjectAddRejectsAMissingPath(t *testing.T) {
	dataDir := t.TempDir()

	out, err := rpcIn(t, dataDir, t.TempDir(), `{"jsonrpc":"2.0","method":"project.add","params":{},"id":1}`)
	if err != nil {
		t.Fatal(err)
	}
	rpcProtocolError(t, out, rpcInvalidParams, "Invalid params")
	if rows := projectRows(t, dataDir); len(rows) != 0 {
		t.Errorf("projects = %v, want none", rows)
	}
}

func TestRPCProjectAddRejectsARelativePath(t *testing.T) {
	dataDir := t.TempDir()

	out, err := rpcIn(t, dataDir, t.TempDir(), `{"jsonrpc":"2.0","method":"project.add","params":{"args":["relative/path"]},"id":1}`)
	if err != nil {
		t.Fatal(err)
	}
	rpcProtocolError(t, out, rpcInvalidParams, "Invalid params")
	if rows := projectRows(t, dataDir); len(rows) != 0 {
		t.Errorf("projects = %v, want none", rows)
	}
}

func TestRPCProjectAddUsesACallerSuppliedAbsolutePath(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)

	result := projectAddResult(t, dataDir, t.TempDir(), repo)
	if result["path"] != repo {
		t.Errorf("result path = %#v, want %q", result["path"], repo)
	}
}

func TestRPCDiscoverProjectAddResultContract(t *testing.T) {
	document := rpcDiscover(t)
	validator := openRPCResultValidator(t, document, "project.add")

	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	result := projectAddResult(t, dataDir, t.TempDir(), repo)
	if err := validator.Validate(result); err != nil {
		t.Fatalf("project.add result does not satisfy the advertised schema: %v", err)
	}

	requiredResultFields(t, validator, result, resultObject, projectResultFields...)
}

func TestRPCDiscoverPublishesProjectAdd(t *testing.T) {
	document := rpcDiscover(t)
	method := openRPCMethodNamed(t, document, "project.add")
	if method.Summary == "" || method.Description == "" {
		t.Errorf("project.add method = %#v, want its help text", method)
	}
	for _, advertised := range document.Methods {
		if advertised.Name == "project" {
			t.Error("discovery advertises the project namespace as a method")
		}
	}
}

func TestProjectArchiveMakesProjectListReportItArchived(t *testing.T) {
	dataDir := t.TempDir()
	registerProjects(t, dataDir, "/srv/alpha", "/srv/zulu")

	if _, err := runIn(t, dataDir, t.TempDir(), "project", "archive", "/srv/zulu"); err != nil {
		t.Fatal(err)
	}

	if got := archivedPaths(t, dataDir); !slices.Equal(got, []string{"/srv/zulu"}) {
		t.Errorf("archived projects = %v, want only /srv/zulu", got)
	}
}

func TestProjectArchiveReturnsTheArchivedRecord(t *testing.T) {
	dataDir := t.TempDir()
	projects := registerProjects(t, dataDir, "/srv/alpha")

	result := projectArchiveResult(t, dataDir, t.TempDir(), "/srv/alpha")

	if result["id"] != float64(projects[0].ID) || result["path"] != "/srv/alpha" ||
		result["default_branch"] != repoBranch {
		t.Errorf("result = %#v, want id %d at /srv/alpha on %s", result, projects[0].ID, repoBranch)
	}
	if result["name"] != "alpha" || result["archived"] != true {
		t.Errorf("result = %#v, want name alpha and archived true", result)
	}
}

func TestProjectArchivePrintsThePathAsTextOutput(t *testing.T) {
	dataDir := t.TempDir()
	registerProjects(t, dataDir, "/srv/alpha")

	out, err := runIn(t, dataDir, t.TempDir(), "project", "archive", "/srv/alpha")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "/srv/alpha") {
		t.Errorf("output = %q, and it does not name the project", out)
	}
}

func TestProjectArchiveDefaultsToTheCurrentWorkingDirectory(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	registerProjects(t, dataDir, projectPath(t, repo))

	if _, err := runIn(t, dataDir, repo, "project", "archive"); err != nil {
		t.Fatal(err)
	}

	if got := archivedPaths(t, dataDir); !slices.Equal(got, []string{projectPath(t, repo)}) {
		t.Errorf("archived projects = %v, want only %q", got, projectPath(t, repo))
	}
}

func TestProjectArchiveTakesAPathRelativeToTheWorkingDirectory(t *testing.T) {
	dataDir := t.TempDir()
	parent := t.TempDir()
	registered := filepath.Join(parent, "widget")
	registerProjects(t, dataDir, registered)

	if _, err := runIn(t, dataDir, parent, "project", "archive", "widget"); err != nil {
		t.Fatal(err)
	}

	if got := archivedPaths(t, dataDir); !slices.Equal(got, []string{registered}) {
		t.Errorf("archived projects = %v, want only %q", got, registered)
	}
}

func TestProjectArchiveFindsARegisteredPathWithNoDirectory(t *testing.T) {
	dataDir := t.TempDir()
	gone := filepath.Join(t.TempDir(), "deleted")
	registerProjects(t, dataDir, gone)

	if _, err := runIn(t, dataDir, t.TempDir(), "project", "archive", gone); err != nil {
		t.Fatal(err)
	}

	if got := archivedPaths(t, dataDir); !slices.Equal(got, []string{gone}) {
		t.Errorf("archived projects = %v, want only %q", got, gone)
	}
}

func TestProjectArchiveLeavesTheTicketsOfTheProjectAlone(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	queued, running := twoTickets(t, dataDir, repo)
	if err := testfix.OpenStore(t, dataDir).ChangeStatus(running, store.Running); err != nil {
		t.Fatal(err)
	}

	if _, err := runIn(t, dataDir, t.TempDir(), "project", "archive", projectPath(t, repo)); err != nil {
		t.Fatal(err)
	}

	if status := testfix.ReadTicket(t, dataDir, queued).Status; status != store.Queued {
		t.Errorf("the queued ticket is %q, want %q", status, store.Queued)
	}
	if status := testfix.ReadTicket(t, dataDir, running).Status; status != store.Running {
		t.Errorf("the running ticket is %q, want %q", status, store.Running)
	}
	box := readInboxJSON(t, dataDir, t.TempDir())
	if got := jsonGroupIDs(t, box, "queued"); !slices.Equal(got, []int64{queued}) {
		t.Errorf("the queued group holds %v, want %v", got, []int64{queued})
	}
	if got := jsonGroupIDs(t, box, "running"); !slices.Equal(got, []int64{running}) {
		t.Errorf("the running group holds %v, want %v", got, []int64{running})
	}
}

func TestProjectArchiveOfAnArchivedProjectIsNotAnError(t *testing.T) {
	dataDir := t.TempDir()
	registerProjects(t, dataDir, "/srv/alpha")

	first := projectArchiveResult(t, dataDir, t.TempDir(), "/srv/alpha")
	second := projectArchiveResult(t, dataDir, t.TempDir(), "/srv/alpha")

	if second["archived"] != true || second["id"] != first["id"] {
		t.Errorf("the second archive returned %#v, want %#v", second, first)
	}
}

func TestProjectArchiveRefusesAPathThatIsNotRegistered(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	registerProjects(t, dataDir, projectPath(t, repo))
	sub := filepath.Join(projectPath(t, repo), "sub")

	_, err := runIn(t, dataDir, t.TempDir(), "project", "archive", sub)

	if !errors.Is(err, store.ErrNoProject) {
		t.Fatalf("err = %v, want %v", err, store.ErrNoProject)
	}
	if code := ErrorCode(err); code != codeNoProject {
		t.Errorf("error code = %d, want %d", code, codeNoProject)
	}
	if got := archivedPaths(t, dataDir); got != nil {
		t.Errorf("archived projects = %v, want none", got)
	}
}

func TestRPCProjectArchiveRejectsARelativePath(t *testing.T) {
	dataDir := t.TempDir()
	registerProjects(t, dataDir, "/srv/alpha")

	out, err := rpcIn(t, dataDir, t.TempDir(),
		`{"jsonrpc":"2.0","method":"project.archive","params":{"args":["relative/path"]},"id":1}`)
	if err != nil {
		t.Fatal(err)
	}
	rpcProtocolError(t, out, rpcInvalidParams, "Invalid params")
	if got := archivedPaths(t, dataDir); got != nil {
		t.Errorf("archived projects = %v, want none", got)
	}
}

func TestRPCProjectArchiveRejectsAMissingPath(t *testing.T) {
	dataDir := t.TempDir()
	registerProjects(t, dataDir, "/srv/alpha")

	out, err := rpcIn(t, dataDir, t.TempDir(),
		`{"jsonrpc":"2.0","method":"project.archive","params":{},"id":1}`)
	if err != nil {
		t.Fatal(err)
	}
	rpcProtocolError(t, out, rpcInvalidParams, "Invalid params")
	if got := archivedPaths(t, dataDir); got != nil {
		t.Errorf("archived projects = %v, want none", got)
	}
}

func TestRPCDiscoverProjectArchiveResultContract(t *testing.T) {
	document := rpcDiscover(t)
	validator := openRPCResultValidator(t, document, "project.archive")

	dataDir := t.TempDir()
	registerProjects(t, dataDir, "/srv/alpha")
	result := projectArchiveResult(t, dataDir, t.TempDir(), "/srv/alpha")
	if err := validator.Validate(result); err != nil {
		t.Fatalf("project.archive result does not satisfy the advertised schema: %v", err)
	}

	requiredResultFields(t, validator, result, resultObject, projectResultFields...)
}

func TestRPCDiscoverPublishesProjectArchive(t *testing.T) {
	document := rpcDiscover(t)
	method := openRPCMethodNamed(t, document, "project.archive")
	if method.Summary == "" || method.Description == "" {
		t.Errorf("project.archive method = %#v, want its help text", method)
	}
}
