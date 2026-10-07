package cli

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"testing"

	"github.com/alcubie/delegator/internal/testfix"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/spf13/cobra"
)

func rpcDiscover(t *testing.T) openRPCDocument {
	t.Helper()
	out, err := rpcIn(t, t.TempDir(), testfix.Repo(t, repoBranch),
		`{"jsonrpc":"2.0","method":"rpc.discover","params":{},"id":"discovery"}`)
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Result openRPCDocument `json:"result"`
		Error  *rpcError       `json:"error"`
		ID     string          `json:"id"`
	}
	if err := json.Unmarshal([]byte(out), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error != nil || response.ID != "discovery" {
		t.Fatalf("rpc.discover response = %#v, want its document and request id", response)
	}
	return response.Result
}

func openRPCMethodNamed(t *testing.T, document openRPCDocument, name string) openRPCMethod {
	t.Helper()
	for _, method := range document.Methods {
		if method.Name == name {
			return method
		}
	}
	t.Fatalf("OpenRPC document has no method %q", name)
	return openRPCMethod{}
}

func openRPCParamNamed(t *testing.T, method openRPCMethod, name string) openRPCContentDescriptor {
	t.Helper()
	for _, param := range method.Params {
		if param.Name == name {
			return param
		}
	}
	t.Fatalf("OpenRPC method %q has no parameter %q", method.Name, name)
	return openRPCContentDescriptor{}
}

// openRPCResultValidator compiles the result schema returned by discovery
// using the JSON Schema draft required by OpenRPC 1.4.1.
func openRPCResultValidator(t *testing.T, document openRPCDocument, method string) *jsonschema.Schema {
	t.Helper()
	schema := openRPCMethodNamed(t, document, method).Result.Schema
	if len(schema) == 0 {
		t.Fatalf("OpenRPC method %q has an empty result schema", method)
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft7)
	compiler.AssertFormat()
	if err := compiler.AddResource("result-schema.json", schema); err != nil {
		t.Fatalf("add %s result schema: %v", method, err)
	}
	validator, err := compiler.Compile("result-schema.json")
	if err != nil {
		t.Fatalf("compile %s result schema: %v", method, err)
	}
	return validator
}

func TestRPCDiscoverDescribesEveryCallableMethod(t *testing.T) {
	document := rpcDiscover(t)
	if document.OpenRPC != openRPCVersion {
		t.Errorf("openrpc = %q, want %q", document.OpenRPC, openRPCVersion)
	}
	if document.Info.Title == "" || document.Info.Version != Version {
		t.Errorf("info = %#v, want a title and dg version %q", document.Info, Version)
	}
	if !slices.IsSortedFunc(document.Methods, func(a, b openRPCMethod) int {
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	}) {
		t.Errorf("methods are not sorted: %#v", document.Methods)
	}

	names := map[string]bool{}
	for _, method := range document.Methods {
		if names[method.Name] {
			t.Errorf("method %q occurs more than once", method.Name)
		}
		names[method.Name] = true
		if method.Result.Name == "" || method.Result.Schema == nil {
			t.Errorf("method %q has no result descriptor: %#v", method.Name, method.Result)
		}
	}
	root := Root(t.TempDir())
	if !names["inbox"] || !names["rpc.discover"] {
		t.Errorf("method names = %v, want inbox and rpc.discover", names)
	}
	rpcVisitCommands(root, func(name string, _ *cobra.Command) {
		if got, want := names[name], !rpcRefusedMethods[name]; got != want {
			t.Errorf("method %q advertised = %t, want %t", name, got, want)
		}
	})
	for refused := range rpcRefusedMethods {
		if names[refused] {
			t.Errorf("refused method %q is advertised", refused)
		}
	}
	for _, nested := range []string{"agents.add", "config.get", "config.list", "config.set"} {
		if !names[nested] {
			t.Errorf("nested method %q is not advertised", nested)
		}
	}
}

func TestRPCDiscoverDescribesArgumentsAndFlags(t *testing.T) {
	document := rpcDiscover(t)
	depend := openRPCMethodNamed(t, document, "depend")
	if depend.ParamStructure != "by-name" {
		t.Errorf("depend paramStructure = %q, want by-name", depend.ParamStructure)
	}
	args := openRPCParamNamed(t, depend, "args")
	if !args.Required || args.Schema["type"] != "array" || args.Schema["minItems"] != float64(1) || args.Schema["maxItems"] != float64(1) {
		t.Errorf("depend args = %#v, want one required array item", args)
	}
	after := openRPCParamNamed(t, depend, "after")
	items, _ := after.Schema["items"].(map[string]any)
	if !after.Required || after.Schema["type"] != "array" || items["type"] != "integer" {
		t.Errorf("depend after = %#v, want a required integer array", after)
	}
	if remove := openRPCParamNamed(t, depend, "remove"); remove.Required || remove.Schema["type"] != "boolean" {
		t.Errorf("depend remove = %#v, want an optional boolean", remove)
	}
	color := openRPCParamNamed(t, depend, "color")
	if color.Schema["type"] != "string" {
		t.Errorf("depend color = %#v, want an inherited string flag", color)
	}
	configGet := openRPCMethodNamed(t, document, "config.get")
	configArgs := openRPCParamNamed(t, configGet, "args")
	if !configArgs.Required || configArgs.Schema["minItems"] != float64(1) || configArgs.Schema["maxItems"] != float64(1) {
		t.Errorf("config.get args = %#v, want one required item", configArgs)
	}
	if configArgs.Description != "The command arguments in the order shown by `dg config get <name>`." {
		t.Errorf("config.get args description = %q, want the qualified command use", configArgs.Description)
	}

	ticketArgs := openRPCParamNamed(t, openRPCMethodNamed(t, document, "ticket"), "args")
	if ticketArgs.Required || ticketArgs.Schema["minItems"] != float64(0) || ticketArgs.Schema["maxItems"] != float64(2) {
		t.Errorf("ticket args = %#v, want zero to two items", ticketArgs)
	}
	edit := openRPCMethodNamed(t, document, "edit")
	for _, param := range edit.Params {
		if param.Name == "editor" {
			t.Errorf("edit advertises the editor parameter that RPC refuses")
		}
	}
	if got := openRPCMethodNamed(t, document, "rpc.discover").Params; len(got) != 0 {
		t.Errorf("rpc.discover params = %#v, want none", got)
	}
}

func TestRPCDiscoverRefusesParameters(t *testing.T) {
	dataDir := t.TempDir()
	workDir := t.TempDir()
	out, err := rpcIn(t, dataDir, workDir,
		`{"jsonrpc":"2.0","method":"rpc.discover","params":{"extra":true},"id":1}`)
	if err != nil {
		t.Fatal(err)
	}
	rpcProtocolError(t, out, rpcInvalidParams, "Invalid params")

	out, err = rpcIn(t, dataDir, workDir,
		`{"jsonrpc":"2.0","method":"rpc.discover","params":[],"id":2}`)
	if err != nil {
		t.Fatal(err)
	}
	got := rpcObject(t, out)
	if got["id"] != float64(2) || got["result"] == nil {
		t.Errorf("empty positional params response = %#v, want the discovery document", got)
	}
}

func TestRPCDiscoverVersionResultContract(t *testing.T) {
	document := rpcDiscover(t)
	validator := openRPCResultValidator(t, document, "version")
	actual := rpcDocument(t, t.TempDir(), t.TempDir(), "version")

	for _, test := range []struct {
		name    string
		result  any
		invalid bool
	}{
		{"actual handler result", actual, false},
		{"additive field", map[string]any{"version": "v1.2.3", "schema": float64(1), "future": true}, false},
		{"missing version", map[string]any{"schema": float64(1)}, true},
		{"wrong schema type", map[string]any{"version": "v1.2.3", "schema": "1"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validator.Validate(test.result)
			if test.invalid && err == nil {
				t.Error("result unexpectedly satisfies the advertised schema")
			}
			if !test.invalid && err != nil {
				t.Errorf("result does not satisfy the advertised schema: %v", err)
			}
		})
	}
}

func TestRPCDiscoverShowResultContract(t *testing.T) {
	document := rpcDiscover(t)
	validator := openRPCResultValidator(t, document, "show")

	queuedDataDir := t.TempDir()
	_, queuedID, queuedRepo := queuedTicket(t, queuedDataDir)
	empty := rpcDocument(t, queuedDataDir, queuedRepo, "show", fmt.Sprint(queuedID))
	if err := validator.Validate(empty); err != nil {
		t.Fatalf("result with absent optional values does not satisfy the advertised schema: %v", err)
	}

	dataDir := t.TempDir()
	s, id, repo := readyTicket(t, dataDir)
	testfix.SetSession(t, s.DataDir(), id, "agent-session-1")
	if err := os.WriteFile(proseFile(dataDir, id), []byte("A ticket body.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	actual := rpcDocument(t, dataDir, repo, "show")
	if err := validator.Validate(actual); err != nil {
		t.Fatalf("implicitly selected populated result does not satisfy the advertised schema: %v", err)
	}

	resultFor := func(t *testing.T, params map[string]any) map[string]any {
		t.Helper()
		request, err := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "method": "show", "params": params, "id": 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		out, err := rpcIn(t, dataDir, repo, string(request))
		if err != nil {
			t.Fatal(err)
		}
		result, ok := rpcObject(t, out)["result"].(map[string]any)
		if !ok {
			t.Fatalf("response = %s, want an object result", out)
		}
		return result
	}
	for _, flag := range []string{"project-only", "ticket-only", "worktree-only", "branch-only", "session-only"} {
		t.Run(flag, func(t *testing.T) {
			result := resultFor(t, map[string]any{"args": []any{float64(id)}, flag: true})
			if err := validator.Validate(result); err != nil {
				t.Errorf("result captured with %s does not satisfy the advertised schema: %v", flag, err)
			}
		})
	}

	if _, err := runIn(t, dataDir, repo, "accept", fmt.Sprint(id), "--force"); err != nil {
		t.Fatal(err)
	}
	accepted := rpcDocument(t, dataDir, repo, "show", fmt.Sprint(id))
	if err := validator.Validate(accepted); err != nil {
		t.Fatalf("result with an acceptance timestamp does not satisfy the advertised schema: %v", err)
	}

	valid := maps.Clone(actual)
	valid["id"] = float64(1 << 62)
	valid["future"] = true
	if err := validator.Validate(valid); err != nil {
		t.Errorf("additive field or wide ticket identifier does not satisfy the advertised schema: %v", err)
	}

	for _, test := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"missing nullable field", func(result map[string]any) { delete(result, "accepted") }},
		{"wrong ticket identifier type", func(result map[string]any) { result["id"] = "1" }},
		{"nonpositive ticket identifier", func(result map[string]any) { result["id"] = float64(0) }},
		{"invalid status", func(result map[string]any) { result["status"] = "waiting" }},
		{"malformed timestamp", func(result map[string]any) { result["created"] = "yesterday" }},
		{"wrong nullable field type", func(result map[string]any) { result["session"] = float64(1) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := maps.Clone(actual)
			test.change(result)
			if err := validator.Validate(result); err == nil {
				t.Error("result unexpectedly satisfies the advertised schema")
			}
		})
	}
}

func TestRPCDiscoverInboxResultContract(t *testing.T) {
	document := rpcDiscover(t)
	validator := openRPCResultValidator(t, document, "inbox")

	emptyDataDir := t.TempDir()
	testfix.OpenStore(t, emptyDataDir)
	empty := rpcDocument(t, emptyDataDir, t.TempDir(), "inbox")
	if err := validator.Validate(empty); err != nil {
		t.Fatalf("empty handler result does not satisfy the advertised schema: %v", err)
	}

	dataDir := t.TempDir()
	_, repo, _ := eachGroup(t, dataDir)
	actual := rpcDocument(t, dataDir, repo, "inbox")
	if err := validator.Validate(actual); err != nil {
		t.Fatalf("populated handler result does not satisfy the advertised schema: %v", err)
	}
	if _, err := runIn(t, dataDir, repo, "pause"); err != nil {
		t.Fatal(err)
	}
	paused := rpcDocument(t, dataDir, repo, "inbox")
	if err := validator.Validate(paused); err != nil {
		t.Fatalf("paused handler result does not satisfy the advertised schema: %v", err)
	}

	queued := one(t, actual, "queued")
	validTicket := maps.Clone(queued)
	validTicket["id"] = float64(1 << 62)
	validTicket["future"] = true
	additive := maps.Clone(empty)
	additive["future"] = true
	additive["queued"] = []any{validTicket}
	if err := validator.Validate(additive); err != nil {
		t.Errorf("additive fields or null timestamps do not satisfy the advertised schema: %v", err)
	}

	for _, test := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"missing queue", func(result map[string]any) { delete(result, "queue") }},
		{"missing done hours", func(result map[string]any) { delete(result, "done_hours") }},
		{"negative done hours", func(result map[string]any) { result["done_hours"] = -1.0 }},
		{"fractional done hours", func(result map[string]any) { result["done_hours"] = 1.5 }},
		{"group is not an array", func(result map[string]any) { result["done"] = map[string]any{} }},
		{"invalid queue", func(result map[string]any) { result["queue"] = "stopped" }},
		{"missing nullable ticket field", func(result map[string]any) {
			row := maps.Clone(queued)
			delete(row, "accepted")
			result["queued"] = []any{row}
		}},
		{"wrong ticket field type", func(result map[string]any) {
			row := maps.Clone(queued)
			row["id"] = "1"
			result["queued"] = []any{row}
		}},
		{"invalid status", func(result map[string]any) {
			row := maps.Clone(queued)
			row["status"] = "waiting"
			result["queued"] = []any{row}
		}},
		{"malformed timestamp", func(result map[string]any) {
			row := maps.Clone(queued)
			row["created"] = "yesterday"
			result["queued"] = []any{row}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := maps.Clone(empty)
			test.change(result)
			if err := validator.Validate(result); err == nil {
				t.Error("result unexpectedly satisfies the advertised schema")
			}
		})
	}
}
