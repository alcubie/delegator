package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

// registerProjects adds projects to a data directory in the given order, as
// the first ticket of a repository registers one, and returns them.
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

func TestProjectListWritesEveryProjectInIDOrder(t *testing.T) {
	dataDir := t.TempDir()
	first, second, third := testfix.Repo(t, repoBranch), testfix.Repo(t, repoBranch), testfix.Repo(t, repoBranch)
	projects := registerProjects(t, dataDir, first, second, third)

	out, err := runIn(t, dataDir, t.TempDir(), "project", "list")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 4 || !strings.HasPrefix(lines[0], "PATH") || !strings.Contains(lines[0], "DEFAULT BRANCH") {
		t.Fatalf("output has no PATH and DEFAULT BRANCH header:\n%s", out)
	}
	for i, project := range projects {
		fields := strings.Fields(lines[i+1])
		if len(fields) != 2 || fields[0] != project.Path || fields[1] != repoBranch {
			t.Errorf("row %d = %q, want %s and %s", i, lines[i+1], project.Path, repoBranch)
		}
	}
}

func TestProjectListValueHoldsEveryFieldInIDOrder(t *testing.T) {
	dataDir := t.TempDir()
	projects := registerProjects(t, dataDir, testfix.Repo(t, repoBranch), testfix.Repo(t, repoBranch))

	result := projectListResult(t, dataDir, t.TempDir())
	if len(result) != len(projects) {
		t.Fatalf("project.list result = %#v, want %d projects", result, len(projects))
	}
	for i, value := range result {
		row, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("project row = %#v, want an object", value)
		}
		if row["id"] != float64(projects[i].ID) || row["path"] != projects[i].Path ||
			row["default_branch"] != repoBranch {
			t.Errorf("row %d = %#v, want project %#v", i, row, projects[i])
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
	registerProjects(t, dataDir, testfix.Repo(t, repoBranch), testfix.Repo(t, repoBranch))
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
