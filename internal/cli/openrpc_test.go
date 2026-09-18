package cli

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/alcubie/delegator/internal/testfix"
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
	for _, command := range root.Commands() {
		if got, want := names[command.Name()], !rpcRefusedMethods[command.Name()]; got != want {
			t.Errorf("method %q advertised = %t, want %t", command.Name(), got, want)
		}
	}
	for refused := range rpcRefusedMethods {
		if names[refused] {
			t.Errorf("refused method %q is advertised", refused)
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
