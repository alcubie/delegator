package cli

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/testfix"
)

// inspectionStore makes an isolated store with one implicitly selectable
// ticket. Keeping the fixtures separate ensures neither command can make the
// other appear to pass through reconciliation side effects.
func inspectionStore(t *testing.T, dataDir, repo string) {
	t.Helper()
	s := testfix.OpenStore(t, dataDir)
	id := queuedIn(t, s, repo, "Inspection command parity")
	finishIn(t, s, id)
}

func TestTicketInspectionCommandsMatchRootCompatibilityForms(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"list project", []string{"list", "--project", "<repo>"}},
		{"search project", []string{"search", "command parity", "--project", "<repo>"}},
		{"show explicit id", []string{"show", "1"}},
		{"show implicit id", []string{"show", "--project", "<repo>"}},
		{"show flag", []string{"show", "1", "--ticket-only"}},
		{"map explicit id", []string{"map", "1", "--mermaid"}},
		{"map implicit id", []string{"map", "--project", "<repo>"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rootData := t.TempDir()
			groupedData := t.TempDir()
			rootRepo := testfix.Repo(t, repoBranch)
			groupedRepo := testfix.Repo(t, repoBranch)
			inspectionStore(t, rootData, rootRepo)
			inspectionStore(t, groupedData, groupedRepo)

			withRepo := func(repo string) []string {
				args := append([]string{}, test.args...)
				for i := range args {
					args[i] = strings.ReplaceAll(args[i], "<repo>", repo)
				}
				return args
			}
			rootArgs := withRepo(rootRepo)
			groupedArgs := append([]string{"ticket"}, withRepo(groupedRepo)...)
			rootOut, rootErr := runIn(t, rootData, rootRepo, rootArgs...)
			groupedOut, groupedErr := runIn(t, groupedData, groupedRepo, groupedArgs...)
			if rootErr != nil || groupedErr != nil {
				t.Fatalf("root error = %v, grouped error = %v", rootErr, groupedErr)
			}
			rootOut = strings.ReplaceAll(rootOut, rootData, "<data-dir>")
			groupedOut = strings.ReplaceAll(groupedOut, groupedData, "<data-dir>")
			rootOut = strings.ReplaceAll(rootOut, rootRepo, "<repo>")
			groupedOut = strings.ReplaceAll(groupedOut, groupedRepo, "<repo>")
			rootOut = strings.ReplaceAll(rootOut, filepath.Base(rootRepo), "<repo>")
			groupedOut = strings.ReplaceAll(groupedOut, filepath.Base(groupedRepo), "<repo>")
			if rootOut != groupedOut {
				t.Errorf("root output:\n%s\ngrouped output:\n%s", rootOut, groupedOut)
			}
		})
	}
}

func TestTicketInspectionCommandsRejectTheSameInvalidArguments(t *testing.T) {
	tests := [][]string{
		{"list", "extra"},
		{"search"},
		{"search", "one", "two"},
		{"show", "1", "2"},
		{"map", "1", "2"},
	}
	for _, args := range tests {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			for _, prefix := range [][]string{nil, {"ticket"}} {
				commandArgs := append(append([]string{}, prefix...), args...)
				if _, err := runIn(t, t.TempDir(), t.TempDir(), commandArgs...); err == nil {
					t.Errorf("dg %s accepted invalid arguments", strings.Join(commandArgs, " "))
				}
			}
		})
	}
}

func TestTicketInspectionRPCMethodsReturnTheSameResults(t *testing.T) {
	tests := []struct {
		name   string
		params func(string) map[string]any
	}{
		{"list", func(repo string) map[string]any { return map[string]any{"project": repo} }},
		{"search", func(repo string) map[string]any {
			return map[string]any{"args": []any{"command parity"}, "project": repo}
		}},
		{"show", func(repo string) map[string]any { return map[string]any{"project": repo} }},
		{"map", func(string) map[string]any { return map[string]any{"args": []any{1}} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dataDir := t.TempDir()
			repo := testfix.Repo(t, repoBranch)
			inspectionStore(t, dataDir, repo)
			result := func(method string) any {
				t.Helper()
				request, err := json.Marshal(map[string]any{
					"jsonrpc": "2.0", "method": method, "params": test.params(repo), "id": method,
				})
				if err != nil {
					t.Fatal(err)
				}
				out, err := rpcIn(t, dataDir, repo, string(request))
				if err != nil {
					t.Fatal(err)
				}
				response := rpcObject(t, out)
				if response["error"] != nil {
					t.Fatalf("%s response = %#v, want a result", method, response)
				}
				return response["result"]
			}
			legacy := result(test.name)
			canonical := result("ticket." + test.name)
			if !reflect.DeepEqual(legacy, canonical) {
				t.Errorf("%s result = %#v, ticket.%s result = %#v", test.name, legacy, test.name, canonical)
			}
		})
	}
}

func TestTicketInspectionCommandFlagsHaveIndependentBindings(t *testing.T) {
	root := Root(t.TempDir())
	for _, test := range []struct {
		command string
		flag    string
		value   string
	}{
		{"list", "project", "/root-list"},
		{"search", "project", "/root-search"},
		{"show", "ticket-only", "true"},
		{"map", "mermaid", "true"},
	} {
		rootCommand, _, err := root.Find([]string{test.command})
		if err != nil {
			t.Fatal(err)
		}
		groupedCommand, _, err := root.Find([]string{"ticket", test.command})
		if err != nil {
			t.Fatal(err)
		}
		before := groupedCommand.Flags().Lookup(test.flag).Value.String()
		if err := rootCommand.Flags().Set(test.flag, test.value); err != nil {
			t.Fatal(err)
		}
		if got := groupedCommand.Flags().Lookup(test.flag).Value.String(); !reflect.DeepEqual(got, before) {
			t.Errorf("setting root %s --%s changed grouped flag from %q to %q", test.command, test.flag, before, got)
		}
	}
}
