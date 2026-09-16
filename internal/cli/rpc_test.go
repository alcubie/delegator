package cli

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/testfix"
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
	if !ok || errorObject["code"] != float64(-32602) || !strings.Contains(fmt.Sprint(errorObject["message"]), "not-a-flag") {
		t.Errorf("invalid flag response = %#v, want invalid-params error naming the flag", got)
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
