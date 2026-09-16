package cli

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/testfix"
	"github.com/spf13/cobra"
)

// rpcIn sends one JSON-RPC request to dg rpc and returns the single line it
// wrote.  The endpoint has one response for a request, including one that
// reports an error, so a caller never has to read command text from stdout.
func rpcIn(t *testing.T, dataDir, workDir, request string) (string, error) {
	t.Helper()
	return runInWithStdin(t, dataDir, workDir, request, "rpc")
}

func rpcObject(t *testing.T, out string) map[string]any {
	t.Helper()
	if strings.Count(out, "\n") != 1 || !strings.HasSuffix(out, "\n") {
		t.Fatalf("dg rpc wrote %q, want one JSON line", out)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("dg rpc wrote invalid JSON %q: %v", out, err)
	}
	return got
}

func rpcProtocolError(t *testing.T, out string, code int, message string) map[string]any {
	t.Helper()
	got := rpcObject(t, out)
	errorObject, ok := got["error"].(map[string]any)
	if !ok || errorObject["code"] != float64(code) || errorObject["message"] != message {
		t.Fatalf("response = %#v, want error %d %q", got, code, message)
	}
	if _, hasResult := got["result"]; hasResult {
		t.Fatalf("error response has result: %#v", got)
	}
	return got
}

func TestRPCReportsProtocolParseAndRequestErrors(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)

	for _, test := range []struct {
		name    string
		request string
		code    int
	}{
		{"text that is not JSON", "not JSON", rpcParseError},
		{"the wrong protocol version", `{"jsonrpc":"1.0","method":"show","id":1}`, rpcInvalidRequest},
		{"a method that is not a string", `{"jsonrpc":"2.0","method":1,"id":1}`, rpcInvalidRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			out, err := rpcIn(t, dataDir, repo, test.request)
			if err != nil {
				t.Fatal(err)
			}
			message := "Invalid Request"
			if test.code == rpcParseError {
				message = "Parse error"
			}
			got := rpcProtocolError(t, out, test.code, message)
			if got["id"] != nil {
				t.Errorf("response id = %#v, want null", got["id"])
			}
		})
	}
}

func TestRPCRefusesAMethodThatNamesNoCommand(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)

	out, err := rpcIn(t, dataDir, repo, `{"jsonrpc":"2.0","method":"not-a-command","id":"missing"}`)
	if err != nil {
		t.Fatal(err)
	}
	got := rpcProtocolError(t, out, rpcMethodNotFound, "Method not found")
	if got["id"] != "missing" {
		t.Errorf("response id = %#v, want the request id", got["id"])
	}
}

func TestRPCRefusesTerminalAndPersonOnlyMethods(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)

	for _, method := range []string{"chat", "rpc"} {
		t.Run(method, func(t *testing.T) {
			out, err := rpcIn(t, dataDir, repo, fmt.Sprintf(`{"jsonrpc":"2.0","method":%q,"id":1}`, method))
			if err != nil {
				t.Fatal(err)
			}
			rpcProtocolError(t, out, rpcMethodNotFound, "Method not found")
		})
	}

	out, err := rpcIn(t, dataDir, repo, `{"jsonrpc":"2.0","method":"edit","params":{"args":[1],"editor":true},"id":2}`)
	if err != nil {
		t.Fatal(err)
	}
	rpcProtocolError(t, out, rpcInvalidParams, "Invalid params")
}

func TestRPCRunsTheNonEditorFormsOfEdit(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	bodyFile := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(bodyFile, []byte("From the body file.\n"), filePerm); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name   string
		params map[string]any
	}{
		{"title", map[string]any{"title": "Changed through RPC"}},
		{"body", map[string]any{"body": "Changed through RPC.\n"}},
		{"body file", map[string]any{"body-file": bodyFile}},
	} {
		t.Run(test.name, func(t *testing.T) {
			id, err := ticketIn(t, dataDir, repo, "Before edit", "Before edit.\n")
			if err != nil {
				t.Fatal(err)
			}
			params := maps.Clone(test.params)
			params["args"] = []any{id}
			request, err := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "method": "edit", "params": params, "id": test.name,
			})
			if err != nil {
				t.Fatal(err)
			}
			out, err := rpcIn(t, dataDir, repo, string(request))
			if err != nil {
				t.Fatal(err)
			}
			got := rpcObject(t, out)
			if _, hasError := got["error"]; hasError {
				t.Errorf("response = %#v, want edit to run", got)
			}
		})
	}
}

func TestRPCRefusesBodyFileFromStandardInputForEveryMethod(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	root := Root(dataDir, repo)

	for _, command := range root.Commands() {
		if command.Name() == "chat" || command.Name() == "rpc" {
			continue
		}
		t.Run(command.Name(), func(t *testing.T) {
			request, err := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "method": command.Name(),
				"params": map[string]any{"body-file": "-"}, "id": command.Name(),
			})
			if err != nil {
				t.Fatal(err)
			}
			out, err := rpcIn(t, dataDir, repo, string(request))
			if err != nil {
				t.Fatal(err)
			}
			rpcProtocolError(t, out, rpcInvalidParams, "Invalid params")
		})
	}
}

func TestRPCEveryCobraCommandIsCallableOrRefused(t *testing.T) {
	root := Root(t.TempDir(), t.TempDir())
	if _, err := rpcTarget(root, "inbox"); err != nil {
		t.Errorf("the root inbox is not callable through RPC: %v", err)
	}

	var walk func(*cobra.Command)
	walk = func(parent *cobra.Command) {
		for _, command := range parent.Commands() {
			_, err := rpcTarget(root, command.Name())
			if rpcRefusedMethods[command.Name()] {
				if err == nil {
					t.Errorf("refused command %q is callable through RPC", command.CommandPath())
				}
			} else if err != nil {
				t.Errorf("command %q is neither callable through RPC nor refused: %v", command.CommandPath(), err)
			}
			walk(command)
		}
	}
	walk(root)
}

func TestRPCShowRunsTheNamedCommandAndWritesOnlyItsResponse(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	id, err := ticketIn(t, dataDir, repo, "Keep the documented value", "The prose of the ticket.")
	if err != nil {
		t.Fatal(err)
	}

	out, err := rpcIn(t, dataDir, repo, fmt.Sprintf(`{"jsonrpc":"2.0","method":"show","params":{"args":[%d]},"id":1}`, id))
	if err != nil {
		t.Fatal(err)
	}
	got := rpcObject(t, out)
	if got["jsonrpc"] != "2.0" || got["id"] != float64(1) {
		t.Errorf("response = %#v, want JSON-RPC 2.0 with id 1", got)
	}
	result, ok := got["result"].(map[string]any)
	if !ok {
		t.Fatalf("response result = %#v, want ticket document", got["result"])
	}
	if result["id"] != float64(id) || result["title"] != "Keep the documented value" {
		t.Errorf("result = %#v, want the document of dg show %d", result, id)
	}
	if _, hasError := got["error"]; hasError {
		t.Errorf("response has error as well as result: %#v", got)
	}
}

func TestRPCPassesArgsAndRepeatedFlags(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	first, err := ticketIn(t, dataDir, repo, "First dependency", "One.")
	if err != nil {
		t.Fatal(err)
	}
	second, err := ticketIn(t, dataDir, repo, "Second dependency", "Two.")
	if err != nil {
		t.Fatal(err)
	}
	third, err := ticketIn(t, dataDir, repo, "Dependent ticket", "Three.")
	if err != nil {
		t.Fatal(err)
	}

	out, err := rpcIn(t, dataDir, repo, fmt.Sprintf(`{"jsonrpc":"2.0","method":"depend","params":{"args":[%d],"after":[%d,%d]},"id":null}`, third, first, second))
	if err != nil {
		t.Fatal(err)
	}
	got := rpcObject(t, out)
	if result, present := got["result"]; !present || result != nil {
		t.Errorf("response result = %#v, want present null", result)
	}
	if got["id"] != nil {
		t.Errorf("response id = %#v, want null", got["id"])
	}
	if got, want := testfix.Dependencies(t, dataDir, third), []int64{first, second}; !slices.Equal(got, want) {
		t.Errorf("dependencies = %v, want %v", got, want)
	}
}

func TestRPCReportsInvalidParamsAndCommandErrors(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)

	out, err := rpcIn(t, dataDir, repo, `{"jsonrpc":"2.0","method":"show","params":{"not-a-flag":1},"id":"flag"}`)
	if err != nil {
		t.Fatal(err)
	}
	got := rpcObject(t, out)
	errorObject, ok := got["error"].(map[string]any)
	if !ok || errorObject["code"] != float64(-32602) || errorObject["message"] != "Invalid params" {
		t.Errorf("invalid flag response = %#v, want the Invalid params protocol error", got)
	}
	if _, hasResult := got["result"]; hasResult {
		t.Errorf("invalid flag response has result: %#v", got)
	}

	out, err = rpcIn(t, dataDir, repo, `{"jsonrpc":"2.0","method":"show","params":{"args":[9999]},"id":"missing"}`)
	if err != nil {
		t.Fatal(err)
	}
	got = rpcObject(t, out)
	errorObject, ok = got["error"].(map[string]any)
	if !ok || errorObject["code"] != float64(codeNoTicket) {
		t.Errorf("failed command response = %#v, want code %d", got, codeNoTicket)
	}
}

func TestRPCBatchKeepsResponseOrder(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)

	out, err := rpcIn(t, dataDir, repo, `[{"jsonrpc":"2.0","method":"version","params":{},"id":1},{"jsonrpc":"2.0","method":"show","params":{"args":[9999]},"id":2}]`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, "\n") != 1 || !strings.HasSuffix(out, "\n") {
		t.Fatalf("dg rpc batch wrote %q, want one JSON line", out)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0]["id"] != float64(1) || got[1]["id"] != float64(2) {
		t.Errorf("batch response = %#v, want two ordered responses", got)
	}
}

func TestRPCListReturnsTheListValueWithoutWritingRows(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	if _, err := ticketIn(t, dataDir, repo, "Listed through RPC", "The value is not parsed from a row."); err != nil {
		t.Fatal(err)
	}

	out, err := rpcIn(t, dataDir, repo, `{"jsonrpc":"2.0","method":"list","params":{},"id":1}`)
	if err != nil {
		t.Fatal(err)
	}
	got := rpcObject(t, out)
	result, ok := got["result"].([]any)
	if !ok || len(result) != 1 {
		t.Fatalf("list result = %#v, want its one ticket", got["result"])
	}
}
