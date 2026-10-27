package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	if len(lines) != len(want)+1 || !strings.HasPrefix(lines[0], "PATH") || !strings.Contains(lines[0], "DEFAULT BRANCH") {
		t.Fatalf("output has no PATH and DEFAULT BRANCH header:\n%s", out)
	}
	for i, path := range want {
		fields := strings.Fields(lines[i+1])
		if len(fields) != 2 || fields[0] != path || fields[1] != repoBranch {
			t.Errorf("row %d = %q, want %s and %s", i, lines[i+1], path, repoBranch)
		}
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

	for _, field := range []string{"id", "path", "default_branch"} {
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

	for _, field := range []string{"id", "path", "default_branch"} {
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
