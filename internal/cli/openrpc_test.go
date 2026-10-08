package cli

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/store"
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

func openRPCMethodsMissingResultSchemas(document openRPCDocument) []string {
	var missing []string
	for _, method := range document.Methods {
		if method.Result.Name == "" || len(method.Result.Schema) == 0 {
			missing = append(missing, method.Name)
		}
	}
	return missing
}

// openRPCResultValidator compiles the result schema returned by discovery
// using the JSON Schema draft required by OpenRPC 1.4.1.
func openRPCResultValidator(t *testing.T, document openRPCDocument, method string) *jsonschema.Schema {
	t.Helper()
	schema := openRPCMethodNamed(t, document, method).Result.Schema
	if len(schema) == 0 {
		t.Fatalf("OpenRPC method %q has an empty result schema", method)
	}
	return openRPCResultValidatorWithCompiler(t, openRPCCompiler(t), document, method)
}

func openRPCCompiler(t *testing.T) *jsonschema.Compiler {
	t.Helper()
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft7)
	compiler.AssertFormat()
	// An empty loader makes a missing transitive fixture fail instead of using
	// the network. These files pin the OpenRPC v1.4.1 schema and its transitive
	// schema resource from meta.json-schema.tools.
	compiler.UseLoader(jsonschema.SchemeURLLoader{})
	for url, path := range map[string]string{
		openRPCMetaSchemaURL:              "testdata/openrpc-1.4-schema.json",
		"https://meta.json-schema.tools":  "testdata/json-schema-tools-meta-schema.json",
		"https://meta.json-schema.tools/": "testdata/json-schema-tools-meta-schema.json",
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read pinned schema %s: %v", path, err)
		}
		var resource any
		if err := json.Unmarshal(data, &resource); err != nil {
			t.Fatalf("decode pinned schema %s: %v", path, err)
		}
		// OpenRPC names its schema-object meta-schema as the dialect. The
		// validator needs the underlying draft named explicitly, while the
		// $refs still validate every Schema Object against that resource.
		resource.(map[string]any)["$schema"] = "http://json-schema.org/draft-07/schema#"
		if err := compiler.AddResource(url, resource); err != nil {
			t.Fatalf("add pinned schema %s: %v", url, err)
		}
	}
	return compiler
}

func openRPCDocumentValidator(t *testing.T, document openRPCDocument) *jsonschema.Schema {
	t.Helper()
	return openRPCResultValidatorWithCompiler(t, openRPCCompiler(t), document, "rpc.discover")
}

func openRPCResultValidatorWithCompiler(t *testing.T, compiler *jsonschema.Compiler, document openRPCDocument, method string) *jsonschema.Schema {
	t.Helper()
	schema := openRPCMethodNamed(t, document, method).Result.Schema
	if err := compiler.AddResource("result-schema.json", schema); err != nil {
		t.Fatalf("add %s result schema: %v", method, err)
	}
	validator, err := compiler.Compile("result-schema.json")
	if err != nil {
		t.Fatalf("compile %s result schema: %v", method, err)
	}
	return validator
}

func rpcNullResult(t *testing.T, validator *jsonschema.Schema, dataDir, workDir, method string, params map[string]any) {
	t.Helper()
	request, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "method": method, "params": params, "id": method,
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := rpcIn(t, dataDir, workDir, string(request))
	if err != nil {
		t.Fatal(err)
	}
	response := rpcObject(t, out)
	result, present := response["result"]
	if !present || result != nil || response["error"] != nil {
		t.Fatalf("%s response = %#v, want a present null result", method, response)
	}
	if err := validator.Validate(result); err != nil {
		t.Fatalf("%s handler result does not satisfy the advertised schema: %v", method, err)
	}
}

func TestRPCDiscoverNullResultContracts(t *testing.T) {
	document := rpcDiscover(t)
	methods := []string{"agents.add", "cancel", "config.set", "depend", "edit", "finish", "move", "pause", "restart", "start", "ticket.depend", "ticket.edit", "ticket.move"}
	validators := make(map[string]*jsonschema.Schema, len(methods))
	for _, method := range methods {
		validators[method] = openRPCResultValidator(t, document, method)
		for _, invalid := range []any{false, float64(0), "", []any{}, map[string]any{}} {
			if err := validators[method].Validate(invalid); err == nil {
				t.Errorf("%s schema accepts non-null result %#v", method, invalid)
			}
		}
	}

	t.Run("edit non-editor forms", func(t *testing.T) {
		dataDir := t.TempDir()
		_, id, repo := queuedTicket(t, dataDir)
		bodyFile := filepath.Join(t.TempDir(), "body.md")
		if err := os.WriteFile(bodyFile, []byte("body from file\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, params := range []map[string]any{
			{"args": []any{id}, "title": "Changed title"},
			{"args": []any{id}, "body": "inline body"},
			{"args": []any{id}, "body-file": bodyFile},
		} {
			rpcNullResult(t, validators["edit"], dataDir, repo, "edit", params)
		}
	})

	t.Run("move", func(t *testing.T) {
		dataDir := t.TempDir()
		_, _, repo := queuedTicket(t, dataDir)
		id := testfix.SecondTicket(t, dataDir)
		rpcNullResult(t, validators["move"], dataDir, repo, "move", map[string]any{"args": []any{id, "top"}})
	})

	t.Run("depend add and remove", func(t *testing.T) {
		dataDir := t.TempDir()
		_, first, repo := queuedTicket(t, dataDir)
		dependent := testfix.SecondTicket(t, dataDir)
		params := map[string]any{"args": []any{dependent}, "after": []any{first}}
		rpcNullResult(t, validators["depend"], dataDir, repo, "depend", params)
		params["remove"] = true
		rpcNullResult(t, validators["depend"], dataDir, repo, "depend", params)
	})

	t.Run("cancel", func(t *testing.T) {
		dataDir := t.TempDir()
		s, id, repo := queuedTicket(t, dataDir)
		if err := s.PauseQueue(); err != nil {
			t.Fatal(err)
		}
		rpcNullResult(t, validators["cancel"], dataDir, repo, "cancel", map[string]any{"args": []any{id}})
	})

	t.Run("restart", func(t *testing.T) {
		dataDir := t.TempDir()
		_, id, repo := failedTicket(t, dataDir)
		launch, record := testfix.RecordingLaunch(t)
		useLaunch(t, launch)
		rpcNullResult(t, validators["restart"], dataDir, repo, "restart", map[string]any{"args": []any{id}})
		testfix.WaitForStarts(t, record, 1)
	})

	t.Run("finish", func(t *testing.T) {
		dataDir := t.TempDir()
		_, id, repo, commit := runningTicket(t, dataDir)
		rpcNullResult(t, validators["finish"], dataDir, repo, "finish", map[string]any{"args": []any{id, commit}})
	})

	t.Run("pause and start", func(t *testing.T) {
		dataDir := t.TempDir()
		workDir := t.TempDir()
		rpcNullResult(t, validators["pause"], dataDir, workDir, "pause", map[string]any{})
		rpcNullResult(t, validators["start"], dataDir, workDir, "start", map[string]any{})
	})

	t.Run("agents add", func(t *testing.T) {
		dataDir := t.TempDir()
		command := executable(t, t.TempDir(), "local-agent")
		rpcNullResult(t, validators["agents.add"], dataDir, t.TempDir(), "agents.add", map[string]any{
			"args": []any{"local"}, "command": command,
		})
	})

	t.Run("config set schedules newly admitted work", func(t *testing.T) {
		dataDir := t.TempDir()
		s, first, repo := queuedTicket(t, dataDir)
		if _, err := s.Claim(first, "delegator/1-first", testAgentID); err != nil {
			t.Fatal(err)
		}
		testfix.SecondTicket(t, dataDir)
		launch, record := testfix.RecordingLaunch(t)
		useLaunch(t, launch)
		rpcNullResult(t, validators["config.set"], dataDir, repo, "config.set", map[string]any{
			"args": []any{"runs", 2},
		})
		testfix.WaitForStarts(t, record, 1)
	})
}

func TestRPCDiscoverTerminalOnlyNullResultContracts(t *testing.T) {
	document := rpcDiscover(t)
	descriptions := map[string]string{
		"agents": "agent list",
		"map":    "dependency map",
		"search": "Search matches",
	}
	validators := make(map[string]*jsonschema.Schema, len(descriptions))
	for method, subject := range descriptions {
		descriptor := openRPCMethodNamed(t, document, method).Result
		if !strings.Contains(descriptor.Description, subject) ||
			!strings.Contains(descriptor.Description, "not yet exposed") ||
			!strings.Contains(descriptor.Description, "null") {
			t.Errorf("%s result description = %q, want the terminal-only structured-result gap", method, descriptor.Description)
		}
		validators[method] = openRPCResultValidator(t, document, method)
		for _, invalid := range []any{false, float64(0), "", []any{}, map[string]any{}} {
			if err := validators[method].Validate(invalid); err == nil {
				t.Errorf("%s schema accepts non-null result %#v", method, invalid)
			}
		}
	}

	t.Run("search matches and no matches", func(t *testing.T) {
		dataDir := t.TempDir()
		repo := testfix.Repo(t, repoBranch)
		if _, err := ticketIn(t, dataDir, repo, "Find the lighthouse", "A searchable ticket.\n"); err != nil {
			t.Fatal(err)
		}
		for _, pattern := range []string{"lighthouse", "absent phrase"} {
			rpcNullResult(t, validators["search"], dataDir, repo, "search", map[string]any{"args": []any{pattern}})
		}
	})

	t.Run("map default and mermaid", func(t *testing.T) {
		dataDir := t.TempDir()
		_, id, repo := queuedTicket(t, dataDir)
		rpcNullResult(t, validators["map"], dataDir, repo, "map", map[string]any{"args": []any{id}})
		rpcNullResult(t, validators["map"], dataDir, repo, "map", map[string]any{"args": []any{id}, "mermaid": true})
	})

	t.Run("agents default and all", func(t *testing.T) {
		dataDir := t.TempDir()
		t.Setenv("PATH", t.TempDir())
		rpcNullResult(t, validators["agents"], dataDir, t.TempDir(), "agents", map[string]any{})
		rpcNullResult(t, validators["agents"], dataDir, t.TempDir(), "agents", map[string]any{"all": true})
	})
}

func TestRPCDiscoverInternalNullResultContracts(t *testing.T) {
	document := rpcDiscover(t)
	descriptions := map[string][]string{
		"run":            {"internal supervisor", "long time", "null"},
		"telemetry-send": {"internal telemetry sender", "consent", "null"},
	}
	validators := make(map[string]*jsonschema.Schema, len(descriptions))
	for method, fragments := range descriptions {
		descriptor := openRPCMethodNamed(t, document, method).Result
		for _, fragment := range fragments {
			if !strings.Contains(descriptor.Description, fragment) {
				t.Errorf("%s result description = %q, want %q", method, descriptor.Description, fragment)
			}
		}
		validators[method] = openRPCResultValidator(t, document, method)
		for _, invalid := range []any{false, float64(0), "", []any{}, map[string]any{}} {
			if err := validators[method].Validate(invalid); err == nil {
				t.Errorf("%s schema accepts non-null result %#v", method, invalid)
			}
		}
	}

	t.Run("run with a paused empty queue", func(t *testing.T) {
		dataDir := t.TempDir()
		s := testfix.OpenStore(t, dataDir)
		if err := s.PauseQueue(); err != nil {
			t.Fatal(err)
		}
		launch, record := testfix.RecordingLaunch(t)
		useLaunch(t, launch)
		rpcNullResult(t, validators["run"], dataDir, t.TempDir(), "run", map[string]any{})
		testfix.WaitForStarts(t, record, 0)
	})

	t.Run("telemetry send with consent disabled", func(t *testing.T) {
		dataDir := t.TempDir()
		s := testfix.OpenStore(t, dataDir)
		if err := s.SetSetting("telemetry", "false"); err != nil {
			t.Fatal(err)
		}
		called := false
		savedSend := sendTelemetry
		sendTelemetry = func(senderStore *store.Store, _ time.Time, _ string) error {
			called = true
			state, err := senderStore.TelemetryState()
			if err != nil {
				return err
			}
			if state.Consent == nil || *state.Consent {
				t.Errorf("telemetry consent = %v, want disabled", state.Consent)
			}
			return fmt.Errorf("network disabled by test")
		}
		t.Cleanup(func() { sendTelemetry = savedSend })

		rpcNullResult(t, validators["telemetry-send"], dataDir, t.TempDir(), "telemetry-send", map[string]any{})
		if !called {
			t.Fatal("telemetry sender was not called")
		}
		state, err := s.TelemetryState()
		if err != nil || state.Consent == nil || *state.Consent {
			t.Fatalf("telemetry consent after send = %v, %v; want disabled", state.Consent, err)
		}
	})
}

func TestRPCInternalCommandErrorsRemainErrorEnvelopes(t *testing.T) {
	dataDir := t.TempDir()
	testfix.OpenStore(t, dataDir)
	requests := []string{
		`{"jsonrpc":"2.0","method":"run","params":{"args":["not-an-id"]},"id":"run"}`,
		`{"jsonrpc":"2.0","method":"telemetry-send","params":{"args":["extra"]},"id":"telemetry"}`,
	}
	for _, request := range requests {
		out, err := rpcIn(t, dataDir, t.TempDir(), request)
		if err != nil {
			t.Fatal(err)
		}
		response := rpcObject(t, out)
		if _, ok := response["error"].(map[string]any); !ok {
			t.Errorf("response = %#v, want an error envelope", response)
		}
		if _, ok := response["result"]; ok {
			t.Errorf("error response has a successful result: %#v", response)
		}
	}
}

func TestRPCTerminalOnlyCommandErrorsRemainErrorEnvelopes(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	requests := []string{
		`{"jsonrpc":"2.0","method":"search","params":{},"id":"search"}`,
		`{"jsonrpc":"2.0","method":"map","params":{"args":[999]},"id":"map"}`,
		`{"jsonrpc":"2.0","method":"agents","params":{"args":["extra"]},"id":"agents"}`,
	}
	for _, request := range requests {
		out, err := rpcIn(t, dataDir, repo, request)
		if err != nil {
			t.Fatal(err)
		}
		response := rpcObject(t, out)
		if _, ok := response["error"].(map[string]any); !ok {
			t.Errorf("response = %#v, want an error envelope", response)
		}
		if _, ok := response["result"]; ok {
			t.Errorf("error response has a successful result: %#v", response)
		}
	}
}

func TestRPCNullResultCommandErrorsRemainErrorEnvelopes(t *testing.T) {
	dataDir := t.TempDir()
	_, id, repo := queuedTicket(t, dataDir)
	requests := []string{
		fmt.Sprintf(`{"jsonrpc":"2.0","method":"edit","params":{"args":[%d]},"id":"edit"}`, id),
		`{"jsonrpc":"2.0","method":"pause","params":{"args":[1]},"id":"pause"}`,
		`{"jsonrpc":"2.0","method":"config.set","params":{"args":["runs",0]},"id":"config"}`,
	}
	for _, request := range requests {
		out, err := rpcIn(t, dataDir, repo, request)
		if err != nil {
			t.Fatal(err)
		}
		response := rpcObject(t, out)
		if _, ok := response["error"].(map[string]any); !ok {
			t.Errorf("response = %#v, want an error envelope", response)
		}
		if _, ok := response["result"]; ok {
			t.Errorf("error response has a successful result: %#v", response)
		}
	}
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
	if missing := openRPCMethodsMissingResultSchemas(document); len(missing) != 0 {
		t.Errorf("methods without result schemas = %v", missing)
	}
	for _, method := range document.Methods {
		if names[method.Name] {
			t.Errorf("method %q occurs more than once", method.Name)
		}
		names[method.Name] = true
		if method.Result.Name == "" || len(method.Result.Schema) == 0 {
			continue
		}
		openRPCResultValidator(t, document, method.Name)
	}
	root := Root(t.TempDir())
	if !names["inbox"] || !names["rpc.discover"] {
		t.Errorf("method names = %v, want inbox and rpc.discover", names)
	}
	rpcVisitCommands(root, func(name string, command *cobra.Command) {
		if got, want := names[name], rpcCallable(command); got != want {
			t.Errorf("method %q advertised = %t, want %t", name, got, want)
		}
	})
	for _, nested := range []string{"agents.add", "config.get", "config.list", "config.set"} {
		if !names[nested] {
			t.Errorf("nested method %q is not advertised", nested)
		}
	}
}

func TestRPCDiscoverPublishesCanonicalTicketMethods(t *testing.T) {
	document := rpcDiscover(t)
	for _, name := range []string{"list", "search", "show", "map", "edit", "move", "depend", "finish", "accept", "cancel", "restart"} {
		legacy := openRPCMethodNamed(t, document, name)
		canonical := openRPCMethodNamed(t, document, "ticket."+name)
		if !strings.Contains(legacy.Description, "preferred command is `dg ticket "+name+"`") ||
			!strings.Contains(legacy.Description, "preferred JSON-RPC method is `ticket."+name+"`") {
			t.Errorf("%s description does not point to its canonical method: %q", name, legacy.Description)
		}
		if !reflect.DeepEqual(legacy.Result.Schema, canonical.Result.Schema) {
			t.Errorf("%s and ticket.%s result schemas differ", name, name)
		}
		if len(legacy.Params) != len(canonical.Params) {
			t.Fatalf("%s has %d parameters and ticket.%s has %d", name, len(legacy.Params), name, len(canonical.Params))
		}
		for i := range legacy.Params {
			legacyParam := legacy.Params[i]
			canonicalParam := canonical.Params[i]
			legacyParam.Description = ""
			canonicalParam.Description = ""
			if !reflect.DeepEqual(legacyParam, canonicalParam) {
				t.Errorf("parameter %d differs between %s and ticket.%s: %#v != %#v", i, name, name, legacyParam, canonicalParam)
			}
		}
	}
}

func TestRPCDiscoverOmitsChatMethods(t *testing.T) {
	document := rpcDiscover(t)
	for _, method := range document.Methods {
		if method.Name == "chat" || method.Name == "ticket.chat" {
			t.Errorf("discovery advertises terminal-only method %q", method.Name)
		}
	}
}

func TestRPCDiscoverDetectsCommandWithoutResultSchema(t *testing.T) {
	root := Root(t.TempDir())
	root.AddCommand(rpcOperationCommand("unregistered", &cobra.Command{Use: "unregistered"}))
	document := rpcOpenRPC(root)
	if missing := openRPCMethodsMissingResultSchemas(document); !slices.Equal(missing, []string{"unregistered"}) {
		t.Fatalf("methods without result schemas = %v, want [unregistered]", missing)
	}
}

func TestRPCNamespaceTraversalUsesOperationPolicy(t *testing.T) {
	root := Root(t.TempDir())
	namespace := &cobra.Command{Use: "group"}
	alias := rpcOperationCommand("pause", &cobra.Command{Use: "stop", Args: cobra.NoArgs})
	interactive := rpcOperationCommand("chat", &cobra.Command{Use: "talk", Args: cobra.NoArgs})
	namespace.AddCommand(alias, interactive)
	root.AddCommand(namespace)

	if _, err := rpcTarget(root, "group"); err == nil {
		t.Error("namespace-only parent resolves as a method")
	}
	if target, err := rpcTarget(root, "group.stop"); err != nil {
		t.Errorf("callable child below a namespace does not resolve: %v", err)
	} else if target != alias {
		t.Errorf("group.stop resolves to %q, want the alias", target.CommandPath())
	}
	if _, err := rpcTarget(root, "group.talk"); err == nil {
		t.Error("interactive operation alias resolves as a method")
	}

	document := rpcOpenRPC(root)
	names := map[string]bool{}
	for _, method := range document.Methods {
		names[method.Name] = true
	}
	if names["group"] || names["group.talk"] || !names["group.stop"] {
		t.Errorf("advertised methods = %v, want only the callable namespace child", names)
	}
	if got, want := openRPCMethodNamed(t, document, "group.stop").Result.Schema, nullResultSchema(); !reflect.DeepEqual(got, want) {
		t.Errorf("alias result schema = %#v, want pause operation schema %#v", got, want)
	}
}

func TestRPCDiscoverDocumentSatisfiesItsResultSchema(t *testing.T) {
	document := rpcDiscover(t)
	method := openRPCMethodNamed(t, document, "rpc.discover")
	if got := method.Result.Schema["$ref"]; got != openRPCMetaSchemaURL {
		t.Fatalf("rpc.discover result reference = %q, want %q", got, openRPCMetaSchemaURL)
	}
	validator := openRPCDocumentValidator(t, document)

	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	var actual map[string]any
	if err := json.Unmarshal(data, &actual); err != nil {
		t.Fatal(err)
	}
	if err := validator.Validate(actual); err != nil {
		t.Fatalf("rpc.discover result does not satisfy its advertised schema: %v", err)
	}

	missingInfo := maps.Clone(actual)
	delete(missingInfo, "info")
	wrongMethods := maps.Clone(actual)
	wrongMethods["methods"] = "not an array"
	for name, invalid := range map[string]any{
		"missing required info": missingInfo,
		"wrong methods type":    wrongMethods,
	} {
		t.Run(name, func(t *testing.T) {
			if err := validator.Validate(invalid); err == nil {
				t.Error("invalid discovery document satisfies the advertised schema")
			}
		})
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

	ticketArgs := openRPCParamNamed(t, openRPCMethodNamed(t, document, "ticket.create"), "args")
	if !ticketArgs.Required || ticketArgs.Schema["minItems"] != float64(1) || ticketArgs.Schema["maxItems"] != float64(2) {
		t.Errorf("ticket.create args = %#v, want one to two items", ticketArgs)
	}
	for _, name := range []string{"edit", "ticket.edit"} {
		edit := openRPCMethodNamed(t, document, name)
		for _, param := range edit.Params {
			if param.Name == "editor" {
				t.Errorf("%s advertises the editor parameter that RPC refuses", name)
			}
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

func TestRPCDiscoverConfigListResultContract(t *testing.T) {
	document := rpcDiscover(t)
	configValidator := openRPCResultValidator(t, document, "config")
	listValidator := openRPCResultValidator(t, document, "config.list")
	if !reflect.DeepEqual(
		openRPCMethodNamed(t, document, "config").Result.Schema,
		openRPCMethodNamed(t, document, "config.list").Result.Schema,
	) {
		t.Fatal("config and config.list advertise different result schemas")
	}

	resultFor := func(t *testing.T, dataDir, method string) []any {
		t.Helper()
		request, err := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "method": method, "params": map[string]any{}, "id": 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		out, err := rpcIn(t, dataDir, t.TempDir(), string(request))
		if err != nil {
			t.Fatal(err)
		}
		result, ok := rpcObject(t, out)["result"].([]any)
		if !ok {
			t.Fatalf("%s result is not an array", method)
		}
		return result
	}
	validateBoth := func(t *testing.T, dataDir string) []any {
		t.Helper()
		var result []any
		for _, method := range []string{"config", "config.list"} {
			result = resultFor(t, dataDir, method)
			for _, validator := range []*jsonschema.Schema{configValidator, listValidator} {
				if err := validator.Validate(result); err != nil {
					t.Fatalf("%s result does not satisfy the advertised schema: %v", method, err)
				}
			}
		}
		return result
	}

	dataDir := t.TempDir()
	actual := validateBoth(t, dataDir)
	if len(actual) != len(config.Definitions) {
		t.Fatalf("config returned %d settings, want %d", len(actual), len(config.Definitions))
	}
	for i, definition := range config.Definitions {
		setting := actual[i].(map[string]any)
		if setting["name"] != definition.Name {
			t.Errorf("setting %d = %v, want %q", i, setting["name"], definition.Name)
		}
	}

	s := testfix.OpenStore(t, dataDir)
	for _, telemetry := range []string{"false", "true"} {
		if err := s.SetSetting("telemetry", telemetry); err != nil {
			t.Fatal(err)
		}
		validateBoth(t, dataDir)
	}
	if err := s.SetSetting("default_model", "provider/model-v1"); err != nil {
		t.Fatal(err)
	}
	validateBoth(t, dataDir)

	for _, invalid := range []any{
		[]any{map[string]any{"name": "runs", "value": float64(2), "description": "numeric strings stay strings"}},
		[]any{map[string]any{"name": "telemetry", "value": "true", "description": "booleans stay booleans"}},
		[]any{map[string]any{"name": "default_model", "value": true, "description": "model"}},
		[]any{map[string]any{"value": "2", "description": "missing name"}},
		[]any{map[string]any{"name": "runs", "value": "2"}},
		[]any{map[string]any{"name": "runs", "description": "missing value"}},
	} {
		if err := configValidator.Validate(invalid); err == nil {
			t.Errorf("invalid result %#v unexpectedly satisfies the advertised schema", invalid)
		}
	}

	out, err := rpcIn(t, t.TempDir(), t.TempDir(),
		`{"jsonrpc":"2.0","method":"config.list","params":{"args":["extra"]},"id":1}`)
	if err != nil {
		t.Fatal(err)
	}
	rpcProtocolError(t, out, 1, `unknown command "extra" for "dg config list"`)
}

func TestRPCDiscoverConfigGetResultContract(t *testing.T) {
	document := rpcDiscover(t)
	method := openRPCMethodNamed(t, document, "config.get")
	validator := openRPCResultValidator(t, document, method.Name)
	if method.Result.Description == "" {
		t.Fatal("config.get result does not explain its parameter-dependent type")
	}

	dataDir, workDir := t.TempDir(), t.TempDir()
	resultFor := func(t *testing.T, name string) any {
		t.Helper()
		request, err := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "method": method.Name,
			"params": map[string]any{"args": []string{name}}, "id": name,
		})
		if err != nil {
			t.Fatal(err)
		}
		out, err := rpcIn(t, dataDir, workDir, string(request))
		if err != nil {
			t.Fatal(err)
		}
		response := rpcObject(t, out)
		result, present := response["result"]
		if !present || response["error"] != nil {
			t.Fatalf("config.get %s response = %#v, want a result", name, response)
		}
		if err := validator.Validate(result); err != nil {
			t.Fatalf("config.get %s result does not satisfy the advertised schema: %v", name, err)
		}
		return result
	}

	for _, definition := range config.Definitions {
		resultFor(t, definition.Name)
	}
	if result := resultFor(t, "default_model"); result != nil {
		t.Errorf("default_model result = %#v, want nullable result", result)
	}
	if result := resultFor(t, "telemetry"); result != nil {
		t.Errorf("telemetry result = %#v, want nullable result", result)
	}

	s := testfix.OpenStore(t, dataDir)
	if err := s.SetSetting("telemetry", "false"); err != nil {
		t.Fatal(err)
	}
	if result := resultFor(t, "telemetry"); result != false {
		t.Errorf("telemetry result = %#v, want false", result)
	}
	if err := s.SetSetting("runs", "42"); err != nil {
		t.Fatal(err)
	}
	if result := resultFor(t, "runs"); result != "42" {
		t.Errorf("runs result = %#v, want numeric-looking string", result)
	}

	for _, invalid := range []any{float64(42), []any{"42"}, map[string]any{"value": "42"}} {
		if err := validator.Validate(invalid); err == nil {
			t.Errorf("invalid result %#v unexpectedly satisfies the advertised schema", invalid)
		}
	}

	out, err := rpcIn(t, dataDir, workDir,
		`{"jsonrpc":"2.0","method":"config.get","params":{"args":["unknown"]},"id":"unknown"}`)
	if err != nil {
		t.Fatal(err)
	}
	rpcProtocolError(t, out, codeUnknown, `unknown setting "unknown"`)
}

func TestRPCDiscoverTicketIDResultContract(t *testing.T) {
	document := rpcDiscover(t)
	ticketValidator := openRPCResultValidator(t, document, "ticket.create")
	acceptValidator := openRPCResultValidator(t, document, "accept")

	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	if _, err := runIn(t, dataDir, repo, "pause"); err != nil {
		t.Fatal(err)
	}
	ticketRequest, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "method": "ticket.create", "id": "ticket",
		"params": map[string]any{
			"args":    []any{"Publish a contract", "Describe the result."},
			"project": repo,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ticketOut, err := rpcIn(t, dataDir, t.TempDir(), string(ticketRequest))
	if err != nil {
		t.Fatal(err)
	}
	ticketResult, ok := rpcObject(t, ticketOut)["result"].(map[string]any)
	if !ok {
		t.Fatalf("ticket response = %s, want an object result", ticketOut)
	}
	if err := ticketValidator.Validate(ticketResult); err != nil {
		t.Fatalf("ticket handler result does not satisfy the advertised schema: %v", err)
	}

	acceptDataDir := t.TempDir()
	s, id, acceptRepo := readyTicket(t, acceptDataDir)
	accepted := commitReadyWork(t, s, acceptDataDir, id)
	testfix.GitIn(t, acceptRepo, "-c", "user.email=test@example.com", "-c", "user.name=Test",
		"merge", "-q", "--no-ff", "-m", "merge ticket", accepted.Branch)
	acceptResult := rpcDocument(t, acceptDataDir, acceptRepo, "accept", fmt.Sprint(id))
	if err := acceptValidator.Validate(acceptResult); err != nil {
		t.Fatalf("accept handler result does not satisfy the advertised schema: %v", err)
	}

	for _, validator := range []*jsonschema.Schema{ticketValidator, acceptValidator} {
		if err := validator.Validate(map[string]any{"id": float64(1), "future": true}); err != nil {
			t.Errorf("additive field does not satisfy the advertised schema: %v", err)
		}
		for _, invalid := range []any{map[string]any{}, map[string]any{"id": "1"}} {
			if err := validator.Validate(invalid); err == nil {
				t.Errorf("invalid result %#v unexpectedly satisfies the advertised schema", invalid)
			}
		}
	}

	errorOut, err := rpcIn(t, t.TempDir(), t.TempDir(),
		`{"jsonrpc":"2.0","method":"ticket.create","params":{"args":["Missing project","Fails."]},"id":1}`)
	if err != nil {
		t.Fatal(err)
	}
	rpcProtocolError(t, errorOut, rpcInvalidParams, "project is required")

	failureDataDir := t.TempDir()
	failureStore, failureID, failureRepo := readyTicket(t, failureDataDir)
	commitReadyWork(t, failureStore, failureDataDir, failureID)
	failureOut, err := rpcIn(t, failureDataDir, failureRepo,
		fmt.Sprintf(`{"jsonrpc":"2.0","method":"accept","params":{"args":[%d]},"id":2}`, failureID))
	if err != nil {
		t.Fatal(err)
	}
	failure := rpcObject(t, failureOut)
	if _, ok := failure["error"].(map[string]any); !ok {
		t.Fatalf("failed accept response = %#v, want an error envelope", failure)
	}
	if _, ok := failure["result"]; ok {
		t.Fatalf("failed accept response has a successful result: %#v", failure)
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
		{"missing depends_on", func(result map[string]any) { delete(result, "depends_on") }},
		{"missing blocks", func(result map[string]any) { delete(result, "blocks") }},
		{"null dependency links", func(result map[string]any) { result["depends_on"] = nil }},
		{"nonpositive dependency identifier", func(result map[string]any) { result["depends_on"] = []any{float64(0)} }},
		{"wrong reverse links type", func(result map[string]any) { result["blocks"] = "2" }},
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
	s, repo, ids := eachGroup(t, dataDir)
	if err := s.AddDependencies(ids["queued"], ids["failed"]); err != nil {
		t.Fatal(err)
	}
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
		{"missing dependencies", func(result map[string]any) {
			row := maps.Clone(queued)
			delete(row, "depends_on")
			result["queued"] = []any{row}
		}},
		{"wrong dependency type", func(result map[string]any) {
			row := maps.Clone(queued)
			row["depends_on"] = []any{"2"}
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

func TestRPCDiscoverListResultContract(t *testing.T) {
	document := rpcDiscover(t)
	validator := openRPCResultValidator(t, document, "list")
	resultFor := func(t *testing.T, dataDir, workDir string, params map[string]any) any {
		t.Helper()
		request, err := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "method": "list", "params": params, "id": 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		out, err := rpcIn(t, dataDir, workDir, string(request))
		if err != nil {
			t.Fatal(err)
		}
		response := rpcObject(t, out)
		if response["error"] != nil {
			t.Fatalf("list response = %#v, want a result", response)
		}
		return response["result"]
	}

	empty := resultFor(t, t.TempDir(), t.TempDir(), map[string]any{})
	if empty != nil {
		t.Fatalf("empty list result = %#v, want null", empty)
	}
	if err := validator.Validate(empty); err != nil {
		t.Fatalf("empty handler result does not satisfy the advertised schema: %v", err)
	}

	dataDir := t.TempDir()
	repo, ids := eachStatus(t, dataDir)
	s := testfix.OpenStore(t, dataDir)
	if err := s.AddDependencies(ids[store.Queued], ids[store.Cancelled]); err != nil {
		t.Fatal(err)
	}
	other := testfix.Repo(t, "release")
	if _, err := ticketIn(t, dataDir, other, "ticket in another project", ""); err != nil {
		t.Fatal(err)
	}

	all, ok := resultFor(t, dataDir, repo, map[string]any{}).([]any)
	if !ok || len(all) != len(ids)+1 {
		t.Fatalf("unfiltered list result = %#v, want every project", all)
	}
	if err := validator.Validate(all); err != nil {
		t.Fatalf("populated handler result does not satisfy the advertised schema: %v", err)
	}
	filtered, ok := resultFor(t, dataDir, repo, map[string]any{"project": repo}).([]any)
	if !ok || len(filtered) != len(ids) {
		t.Fatalf("filtered list result = %#v, want the six tickets in %s", filtered, repo)
	}
	if err := validator.Validate(filtered); err != nil {
		t.Fatalf("project-filtered result does not satisfy the advertised schema: %v", err)
	}

	byStatus := map[string]map[string]any{}
	for _, value := range filtered {
		row, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("list row = %#v, want an object", value)
		}
		byStatus[row["Status"].(string)] = row
	}
	for status := range ids {
		if byStatus[string(status)] == nil {
			t.Errorf("filtered result omitted status %q", status)
		}
	}
	queued := byStatus[string(store.Queued)]
	if queued["Accepted"] != "0001-01-01T00:00:00Z" || queued["Started"] != "0001-01-01T00:00:00Z" {
		t.Errorf("queued zero times = Accepted %#v, Started %#v", queued["Accepted"], queued["Started"])
	}
	if got := queued["DependsOn"]; !slices.Equal(got.([]any), []any{float64(ids[store.Cancelled])}) {
		t.Errorf("queued DependsOn = %#v, want cancelled ticket %d", got, ids[store.Cancelled])
	}
	if byStatus[string(store.Running)]["Position"] != float64(0) || queued["Position"] == float64(0) {
		t.Errorf("positions do not preserve list membership: queued %#v, running %#v", queued["Position"], byStatus[string(store.Running)]["Position"])
	}

	valid := maps.Clone(queued)
	valid["future"] = true
	if err := validator.Validate([]any{valid}); err != nil {
		t.Errorf("additive ticket field does not satisfy the advertised schema: %v", err)
	}
	for _, test := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"missing exported field", func(row map[string]any) { delete(row, "Project") }},
		{"wrong position type", func(row map[string]any) { row["Position"] = "first" }},
		{"invalid status", func(row map[string]any) { row["Status"] = "waiting" }},
		{"null timestamp", func(row map[string]any) { row["Accepted"] = nil }},
		{"wrong dependency type", func(row map[string]any) { row["DependsOn"] = []any{"2"} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			row := maps.Clone(queued)
			test.change(row)
			if err := validator.Validate([]any{row}); err == nil {
				t.Error("result unexpectedly satisfies the advertised schema")
			}
		})
	}

	request, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "method": "list",
		"params": map[string]any{"project": filepath.Join(t.TempDir(), "missing")}, "id": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := rpcIn(t, dataDir, repo, string(request))
	if err != nil {
		t.Fatal(err)
	}
	errorResponse := rpcObject(t, out)
	if _, ok := errorResponse["error"].(map[string]any); !ok {
		t.Fatalf("failed list response = %#v, want an error envelope", errorResponse)
	}
	if _, ok := errorResponse["result"]; ok {
		t.Fatalf("failed list response has a successful result: %#v", errorResponse)
	}
}
