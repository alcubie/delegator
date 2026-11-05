package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/project"
	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

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

// projectAddResult reads the object that project.add returns through the
// protocol for the repository at dir.
func projectAddResult(t *testing.T, dataDir, workDir, dir string) map[string]any {
	t.Helper()
	request := fmt.Sprintf(`{"jsonrpc":"2.0","method":"project.add","params":{"args":[%q]},"id":1}`, dir)
	out, err := rpcIn(t, dataDir, workDir, request)
	if err != nil {
		t.Fatal(err)
	}
	response := rpcObject(t, out)
	if response["error"] != nil {
		t.Fatalf("project.add response = %#v, want a result", response)
	}
	result, ok := response["result"].(map[string]any)
	if !ok {
		t.Fatalf("project.add result = %#v, want an object", response["result"])
	}
	return result
}

// projectRenameResult reads the object that project.rename returns through the
// protocol for the given params.
func projectRenameResult(t *testing.T, dataDir, workDir, params string) map[string]any {
	t.Helper()
	request := fmt.Sprintf(`{"jsonrpc":"2.0","method":"project.rename","params":%s,"id":1}`, params)
	out, err := rpcIn(t, dataDir, workDir, request)
	if err != nil {
		t.Fatal(err)
	}
	response := rpcObject(t, out)
	if response["error"] != nil {
		t.Fatalf("project.rename response = %#v, want a result", response)
	}
	result, ok := response["result"].(map[string]any)
	if !ok {
		t.Fatalf("project.rename result = %#v, want an object", response["result"])
	}
	return result
}

// listedName reads the name that project.list gives the project at path, so a
// rename is observed through the listing rather than through the database.
func listedName(t *testing.T, dataDir, path string) string {
	t.Helper()
	for _, value := range projectListResult(t, dataDir, t.TempDir()) {
		row, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("project row = %#v, want an object", value)
		}
		if row["path"] != path {
			continue
		}
		name, ok := row["name"].(string)
		if !ok {
			t.Fatalf("project row = %#v, want a name", row)
		}
		return name
	}
	t.Fatalf("project.list holds no project at %s", path)
	return ""
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

	for _, field := range []string{"id", "name", "path", "default_branch", "archived"} {
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		var copied []any
		if err := json.Unmarshal(encoded, &copied); err != nil {
			t.Fatal(err)
		}
		delete(copied[0].(map[string]any), field)
		if err := validator.Validate(copied); err == nil {
			t.Errorf("a result missing %q satisfies the schema", field)
		}
	}
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

	for _, field := range []string{"id", "name", "path", "default_branch", "archived"} {
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		var copied map[string]any
		if err := json.Unmarshal(encoded, &copied); err != nil {
			t.Fatal(err)
		}
		delete(copied, field)
		if err := validator.Validate(copied); err == nil {
			t.Errorf("a result missing %q satisfies the schema", field)
		}
	}
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

func TestProjectRenameChangesTheNameOfTheListedProject(t *testing.T) {
	dataDir := t.TempDir()
	registerProjects(t, dataDir, "/srv/sleep/backend")

	out, err := runIn(t, dataDir, t.TempDir(), "project", "rename", "Sleep backend",
		"--project", "/srv/sleep/backend")
	if err != nil {
		t.Fatal(err)
	}
	if want := "Sleep backend\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if got := listedName(t, dataDir, "/srv/sleep/backend"); got != "Sleep backend" {
		t.Errorf("name = %q, want Sleep backend", got)
	}
}

func TestProjectRenameClearsTheNameBackToTheFolderName(t *testing.T) {
	dataDir := t.TempDir()
	registerProjects(t, dataDir, "/srv/sleep/backend")
	if _, err := runIn(t, dataDir, t.TempDir(), "project", "rename", "Sleep backend",
		"--project", "/srv/sleep/backend"); err != nil {
		t.Fatal(err)
	}

	out, err := runIn(t, dataDir, t.TempDir(), "project", "rename", "--clear",
		"--project", "/srv/sleep/backend")
	if err != nil {
		t.Fatal(err)
	}
	if want := "backend\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if got := listedName(t, dataDir, "/srv/sleep/backend"); got != "backend" {
		t.Errorf("name = %q, want the folder name backend", got)
	}
}

// The path matches one project and not every project whose path begins with
// it, so renaming /srv/sleep/backend leaves /srv/sleep/backend-api alone.
func TestProjectRenameNamesOnlyTheProjectAtThePath(t *testing.T) {
	dataDir := t.TempDir()
	registerProjects(t, dataDir, "/srv/sleep/backend", "/srv/sleep/backend-api")

	if _, err := runIn(t, dataDir, t.TempDir(), "project", "rename", "Sleep backend",
		"--project", "/srv/sleep/backend"); err != nil {
		t.Fatal(err)
	}
	if got := listedName(t, dataDir, "/srv/sleep/backend-api"); got != "backend-api" {
		t.Errorf("name of the sibling = %q, want the folder name backend-api", got)
	}
}

// The directory of --project matches a registered path and nothing below it,
// because dg project add registers the root of a repository.
func TestProjectRenameDoesNotMatchASubdirectoryOfTheProject(t *testing.T) {
	dataDir := t.TempDir()
	registerProjects(t, dataDir, "/srv/sleep/backend")

	_, err := runIn(t, dataDir, t.TempDir(), "project", "rename", "Sleep backend",
		"--project", "/srv/sleep/backend/internal")
	if !errors.Is(err, store.ErrNoProject) {
		t.Fatalf("err = %v, want %v", err, store.ErrNoProject)
	}
	if got := listedName(t, dataDir, "/srv/sleep/backend"); got != "backend" {
		t.Errorf("name = %q, and a subdirectory renamed the project", got)
	}
}

func TestProjectRenameNamesAProjectWhoseDirectoryIsGone(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	if _, err := runIn(t, dataDir, repo, "project", "add"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}

	if _, err := runIn(t, dataDir, t.TempDir(), "project", "rename", "Widget", "--project", repo); err != nil {
		t.Fatal(err)
	}
	if got := listedName(t, dataDir, repo); got != "Widget" {
		t.Errorf("name = %q, want Widget", got)
	}
}

func TestProjectRenameDefaultsToTheWorkingDirectory(t *testing.T) {
	dataDir := t.TempDir()
	workDir := t.TempDir()
	registerProjects(t, dataDir, workDir)

	if _, err := runIn(t, dataDir, workDir, "project", "rename", "Widget"); err != nil {
		t.Fatal(err)
	}
	if got := listedName(t, dataDir, workDir); got != "Widget" {
		t.Errorf("name = %q, want Widget", got)
	}
}

// A relative --project resolves against the working directory of the command,
// as the relative directory of every other command does.
func TestProjectRenameResolvesARelativeProjectDirectory(t *testing.T) {
	dataDir := t.TempDir()
	parent := t.TempDir()
	registered := filepath.Join(parent, "backend")
	registerProjects(t, dataDir, registered)

	if _, err := runIn(t, dataDir, parent, "project", "rename", "Widget", "--project", "backend"); err != nil {
		t.Fatal(err)
	}
	if got := listedName(t, dataDir, registered); got != "Widget" {
		t.Errorf("name = %q, want Widget", got)
	}
}

func TestProjectRenameTrimsTheName(t *testing.T) {
	dataDir := t.TempDir()
	registerProjects(t, dataDir, "/srv/sleep/backend")

	if _, err := runIn(t, dataDir, t.TempDir(), "project", "rename", "  Sleep backend  ",
		"--project", "/srv/sleep/backend"); err != nil {
		t.Fatal(err)
	}
	if got := listedName(t, dataDir, "/srv/sleep/backend"); got != "Sleep backend" {
		t.Errorf("name = %q, want it trimmed to Sleep backend", got)
	}
}

// A person who names two projects the same has a reason the command cannot
// know, so a duplicate is stored rather than refused.
func TestProjectRenameAllowsTwoProjectsTheSameName(t *testing.T) {
	dataDir := t.TempDir()
	registerProjects(t, dataDir, "/srv/health/backend", "/srv/sleep/backend")

	for _, path := range []string{"/srv/health/backend", "/srv/sleep/backend"} {
		if _, err := runIn(t, dataDir, t.TempDir(), "project", "rename", "Backend", "--project", path); err != nil {
			t.Fatalf("renaming %s: %v", path, err)
		}
	}
	for _, path := range []string{"/srv/health/backend", "/srv/sleep/backend"} {
		if got := listedName(t, dataDir, path); got != "Backend" {
			t.Errorf("name of %s = %q, want Backend", path, got)
		}
	}
}

func TestProjectRenameRefusesWhatItCannotStore(t *testing.T) {
	for _, refusal := range []struct {
		name string
		args []string
		err  error
		code int
	}{
		{"an unregistered path", []string{"Backend", "--project", "/srv/elsewhere"},
			store.ErrNoProject, codeNoProject},
		{"a name of only space", []string{"   ", "--project", "/srv/sleep/backend"},
			errNoProjectName, codeNoProjectName},
		{"no name and no --clear", []string{"--project", "/srv/sleep/backend"},
			errNoProjectName, codeNoProjectName},
		{"a name holding a line break", []string{"Sleep\nbackend", "--project", "/srv/sleep/backend"},
			errProjectNameLineBreak, codeProjectNameLineBreak},
		{"a name and --clear together", []string{"Backend", "--clear", "--project", "/srv/sleep/backend"},
			errProjectNameAndClear, codeProjectNameAndClear},
	} {
		t.Run(refusal.name, func(t *testing.T) {
			dataDir := t.TempDir()
			registerProjects(t, dataDir, "/srv/sleep/backend")

			_, err := runIn(t, dataDir, t.TempDir(), append([]string{"project", "rename"}, refusal.args...)...)
			if !errors.Is(err, refusal.err) {
				t.Fatalf("err = %v, want %v", err, refusal.err)
			}
			if got := ErrorCode(err); got != refusal.code {
				t.Errorf("code = %d, want %d", got, refusal.code)
			}
			if got := listedName(t, dataDir, "/srv/sleep/backend"); got != "backend" {
				t.Errorf("name = %q, and the refused rename changed it", got)
			}
		})
	}
}

func TestRPCDiscoverProjectRenameResultContract(t *testing.T) {
	document := rpcDiscover(t)
	validator := openRPCResultValidator(t, document, "project.rename")

	dataDir := t.TempDir()
	registerProjects(t, dataDir, "/srv/sleep/backend")
	result := projectRenameResult(t, dataDir, t.TempDir(),
		`{"args":["Sleep backend"],"project":"/srv/sleep/backend"}`)
	if result["name"] != "Sleep backend" || result["path"] != "/srv/sleep/backend" {
		t.Errorf("result = %#v, want Sleep backend at /srv/sleep/backend", result)
	}
	if err := validator.Validate(result); err != nil {
		t.Fatalf("project.rename result does not satisfy the advertised schema: %v", err)
	}

	for _, field := range []string{"id", "name", "path", "default_branch", "archived"} {
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		var copied map[string]any
		if err := json.Unmarshal(encoded, &copied); err != nil {
			t.Fatal(err)
		}
		delete(copied, field)
		if err := validator.Validate(copied); err == nil {
			t.Errorf("a result missing %q satisfies the schema", field)
		}
	}
}

func TestRPCProjectRenameReportsTheCodeOfAnUnregisteredPath(t *testing.T) {
	dataDir := t.TempDir()

	out, err := rpcIn(t, dataDir, t.TempDir(),
		`{"jsonrpc":"2.0","method":"project.rename","params":{"args":["Backend"],"project":"/srv/elsewhere"},"id":1}`)
	if err != nil {
		t.Fatal(err)
	}
	response := rpcObject(t, out)
	failure, ok := response["error"].(map[string]any)
	if !ok || failure["code"] != float64(codeNoProject) {
		t.Errorf("response = %#v, want error code %d", response, codeNoProject)
	}
}

func TestRPCDiscoverPublishesProjectRename(t *testing.T) {
	document := rpcDiscover(t)
	method := openRPCMethodNamed(t, document, "project.rename")
	if method.Summary == "" || method.Description == "" {
		t.Errorf("project.rename method = %#v, want its help text", method)
	}
}
