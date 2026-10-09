package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/testfix"
	"github.com/spf13/cobra"
)

// rpcIn submits one JSON-RPC request and returns its response line, including
// protocol or command errors.
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

func TestRPCDoesNotExposeNamespacesOrOpenAnEditor(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	for _, request := range []string{
		`{"jsonrpc":"2.0","method":"ticket","id":"namespace"}`,
		`{"jsonrpc":"2.0","method":"queue","id":"queue-namespace"}`,
		fmt.Sprintf(`{"jsonrpc":"2.0","method":"ticket.create","params":{"project":%q},"id":"editor"}`, repo),
		fmt.Sprintf(`{"jsonrpc":"2.0","method":"ticket.create","params":{"args":[],"project":%q},"id":"empty-args"}`, repo),
		fmt.Sprintf(`{"jsonrpc":"2.0","method":"ticket.create","params":{"args":["stdin body"],"project":%q,"body-file":"-"},"id":"stdin"}`, repo),
	} {
		out, err := rpcIn(t, dataDir, t.TempDir(), request)
		if err != nil {
			t.Fatal(err)
		}
		got := rpcObject(t, out)
		if _, ok := got["error"].(map[string]any); !ok {
			t.Errorf("response = %#v, want an error", got)
		}
	}
	if tickets, err := testfix.OpenStore(t, dataDir).AllTickets(""); err != nil {
		t.Fatal(err)
	} else if len(tickets) != 0 {
		t.Errorf("rejected requests created %d tickets, want none", len(tickets))
	}
}

func TestRPCRefusesTerminalAndPersonOnlyMethods(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)

	for _, method := range []string{"chat", "ticket.chat", "init", "rpc"} {
		t.Run(method, func(t *testing.T) {
			out, err := rpcIn(t, dataDir, repo, fmt.Sprintf(`{"jsonrpc":"2.0","method":%q,"id":1}`, method))
			if err != nil {
				t.Fatal(err)
			}
			rpcProtocolError(t, out, rpcMethodNotFound, "Method not found")
		})
	}

	for _, method := range []string{"edit", "ticket.edit"} {
		for _, params := range []string{`{"args":[1],"editor":true}`, `{"args":[1],"body-file":"-"}`} {
			request := fmt.Sprintf(`{"jsonrpc":"2.0","method":%q,"params":%s,"id":2}`, method, params)
			out, err := rpcIn(t, dataDir, repo, request)
			if err != nil {
				t.Fatal(err)
			}
			rpcProtocolError(t, out, rpcInvalidParams, "Invalid params")
		}
	}
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
		for _, method := range []string{"edit", "ticket.edit"} {
			t.Run(test.name+" through "+method, func(t *testing.T) {
				id, err := ticketIn(t, dataDir, repo, "Before edit", "Before edit.\n")
				if err != nil {
					t.Fatal(err)
				}
				params := maps.Clone(test.params)
				params["args"] = []any{id}
				request, err := json.Marshal(map[string]any{
					"jsonrpc": "2.0", "method": method, "params": params, "id": test.name,
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
}

func TestRPCRefusesBodyFileFromStandardInputForEveryMethod(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	root := Root(repo)

	for _, command := range root.Commands() {
		if !rpcCallable(command) {
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
	root := Root(t.TempDir())
	if _, err := rpcTarget(root, "inbox"); err != nil {
		t.Errorf("the root inbox is not callable through RPC: %v", err)
	}

	var walk func(*cobra.Command)
	walk = func(parent *cobra.Command) {
		for _, command := range parent.Commands() {
			method := rpcMethodName(command)
			target, err := rpcTarget(root, method)
			operation, registered := rpcOperation(command)
			if !registered && command.Runnable() && len(command.Commands()) == 0 {
				t.Errorf("runnable command %q has no RPC operation identity", command.CommandPath())
			}
			if !registered || rpcRefusedOperations[operation] {
				if err == nil {
					t.Errorf("non-callable command %q is callable through RPC", command.CommandPath())
				}
			} else if err != nil {
				t.Errorf("command %q is neither callable through RPC nor refused: %v", command.CommandPath(), err)
			} else if target != command {
				t.Errorf("method %q resolves to %q, want %q", method, target.CommandPath(), command.CommandPath())
			}
			walk(command)
		}
	}
	walk(root)
}

func TestRPCArgumentsCannotRetargetResolvedOperations(t *testing.T) {
	root := Root(t.TempDir())
	tests := []struct {
		method string
		params string
		want   []string
	}{
		{"inbox", `{"args":["show"]}`, []string{"--", "show"}},
		{"config", `{"args":["get"]}`, []string{"config", "--", "get"}},
		{"show", `{"args":["-1"]}`, []string{"show", "--", "-1"}},
		{"ticket.create", `{"args":["runs"],"project":"/project"}`, []string{"ticket", "create", "--project", "/project", "--", "runs"}},
	}
	for _, test := range tests {
		t.Run(test.method, func(t *testing.T) {
			command, err := rpcTarget(root, test.method)
			if err != nil {
				t.Fatal(err)
			}
			got, err := rpcArgv(command, rpcRequest{Params: json.RawMessage(test.params)})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, test.want) {
				t.Errorf("argv = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRPCOperationRestrictionsFollowAliases(t *testing.T) {
	group := &cobra.Command{Use: "group"}
	create := rpcOperationCommand("ticket", &cobra.Command{Use: "create [title]"})
	create.Flags().String("project", "", "project")
	edit := rpcOperationCommand("edit", &cobra.Command{Use: "change <id>"})
	edit.Flags().Bool("editor", false, "editor")
	group.AddCommand(create, edit)
	root := &cobra.Command{Use: "dg"}
	root.AddCommand(group)

	resolved, err := rpcTarget(root, "group.create")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rpcArgv(resolved, rpcRequest{}); err == nil {
		t.Error("creation alias accepts an omitted project")
	} else if _, ok := err.(rpcProjectRequiredError); !ok {
		t.Errorf("creation alias error = %T %v, want rpcProjectRequiredError", err, err)
	}

	resolved, err = rpcTarget(root, "group.change")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rpcArgv(resolved, rpcRequest{Params: json.RawMessage(`{"editor":true}`)}); err == nil {
		t.Error("edit alias accepts editor mode")
	}
}

func TestRPCCobraGeneratedHelpersRemainUnavailable(t *testing.T) {
	generated := []string{
		"help",
		"completion",
		"completion.bash",
		"completion.fish",
		"completion.powershell",
		"completion.zsh",
		cobra.ShellCompRequestCmd,
		cobra.ShellCompNoDescRequestCmd,
	}

	// Cobra installs help and completion only when Execute starts. RPC resolves
	// against a fresh tree before that point, so publishing these methods would
	// claim commands that a request cannot reach.
	root := Root(t.TempDir())
	document := rpcOpenRPC(root)
	advertised := make(map[string]bool, len(document.Methods))
	for _, method := range document.Methods {
		advertised[method.Name] = true
	}
	for _, method := range generated {
		if advertised[method] {
			t.Errorf("generated helper %q is advertised", method)
		}
		if _, err := rpcTarget(root, method); err == nil {
			t.Errorf("generated helper %q resolves before Cobra initialization", method)
		}
	}

	runtimeRoot := Root(t.TempDir())
	runtimeRoot.SetOut(io.Discard)
	runtimeRoot.SetErr(io.Discard)
	runtimeRoot.SetArgs([]string{"completion", "bash"})
	if err := runtimeRoot.Execute(); err != nil {
		t.Fatal(err)
	}
	runtimeMethods := map[string]bool{}
	rpcVisitCommands(runtimeRoot, func(name string, _ *cobra.Command) {
		runtimeMethods[name] = true
	})
	for _, method := range generated[:6] {
		if !runtimeMethods[method] {
			t.Errorf("normal Cobra runtime tree has no generated helper %q", method)
		}
	}
	hiddenRoot := Root(t.TempDir())
	hiddenRoot.SetOut(io.Discard)
	hiddenRoot.SetErr(io.Discard)
	hiddenRoot.SetArgs([]string{cobra.ShellCompRequestCmd, ""})
	if err := hiddenRoot.Execute(); err != nil {
		t.Fatal(err)
	}
	var hidden *cobra.Command
	rpcVisitCommands(hiddenRoot, func(name string, command *cobra.Command) {
		if name == cobra.ShellCompRequestCmd {
			hidden = command
		}
	})
	if hidden == nil {
		t.Errorf("normal Cobra completion request has no generated helper %q", cobra.ShellCompRequestCmd)
	} else if !hidden.HasAlias(cobra.ShellCompNoDescRequestCmd) {
		t.Errorf("generated helper %q has no %q alias", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd)
	}

	dataDir := t.TempDir()
	workDir := t.TempDir()
	for _, method := range generated {
		request := fmt.Sprintf(`{"jsonrpc":"2.0","method":%q,"id":1}`, method)
		response, err := rpcIn(t, dataDir, workDir, request)
		if err != nil {
			t.Fatal(err)
		}
		rpcProtocolError(t, response, rpcMethodNotFound, "Method not found")
	}
}

func TestRPCNestedMethodsUseQualifiedNamesForDispatchAndParameters(t *testing.T) {
	dataDir := t.TempDir()
	workDir := t.TempDir()

	out, err := rpcIn(t, dataDir, workDir,
		`{"jsonrpc":"2.0","method":"config.set","params":{"args":["runs",4]},"id":"set"}`)
	if err != nil {
		t.Fatal(err)
	}
	response := rpcObject(t, out)
	if result, present := response["result"]; !present || result != nil {
		t.Fatalf("config.set response = %#v, want a null result", response)
	}

	out, err = rpcIn(t, dataDir, workDir,
		`{"jsonrpc":"2.0","method":"config.get","params":{"args":["runs"]},"id":"get"}`)
	if err != nil {
		t.Fatal(err)
	}
	response = rpcObject(t, out)
	if response["result"] != "4" || response["id"] != "get" {
		t.Errorf("config.get response = %#v, want stored value 4", response)
	}

	out, err = rpcIn(t, dataDir, workDir,
		`{"jsonrpc":"2.0","method":"get","params":{"args":["runs"]},"id":"unqualified"}`)
	if err != nil {
		t.Fatal(err)
	}
	rpcProtocolError(t, out, rpcMethodNotFound, "Method not found")
}

func TestRPCNestedMethodErrorsKeepProtocolAndCommandCodes(t *testing.T) {
	dataDir := t.TempDir()
	workDir := t.TempDir()

	out, err := rpcIn(t, dataDir, workDir,
		`{"jsonrpc":"2.0","method":"config.get","params":{"unknown":true},"id":"params"}`)
	if err != nil {
		t.Fatal(err)
	}
	rpcProtocolError(t, out, rpcInvalidParams, "Invalid params")

	out, err = rpcIn(t, dataDir, workDir,
		`{"jsonrpc":"2.0","method":"config.get","params":{"args":["unknown"]},"id":"command"}`)
	if err != nil {
		t.Fatal(err)
	}
	response := rpcProtocolError(t, out, codeUnknown, `unknown setting "unknown"`)
	if response["id"] != "command" {
		t.Errorf("command error id = %#v, want command", response["id"])
	}
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

func TestRPCTicketCreateRequiresAProjectAndUsesIt(t *testing.T) {
	dataDir := t.TempDir()
	workDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)

	out, err := rpcIn(t, dataDir, workDir, `{"jsonrpc":"2.0","method":"ticket.create","params":{"args":["No implicit project","The request must name one."]},"id":"missing-project"}`)
	if err != nil {
		t.Fatal(err)
	}
	got := rpcProtocolError(t, out, rpcInvalidParams, "project is required")
	if got["id"] != "missing-project" {
		t.Errorf("response id = %#v, want the request id", got["id"])
	}
	if tickets, err := testfix.OpenStore(t, dataDir).AllTickets(""); err != nil {
		t.Fatal(err)
	} else if len(tickets) != 0 {
		t.Errorf("rejected request created %d tickets, want none", len(tickets))
	}

	out, err = rpcIn(t, dataDir, workDir, fmt.Sprintf(`{"jsonrpc":"2.0","method":"ticket.create","params":{"args":["Explicit project","The request named its project."],"project":%q},"id":1}`, repo))
	if err != nil {
		t.Fatal(err)
	}
	response := rpcObject(t, out)
	result, ok := response["result"].(map[string]any)
	if !ok {
		t.Fatalf("response = %#v, want a ticket id", response)
	}
	id, ok := result["id"].(float64)
	if !ok {
		t.Fatalf("result = %#v, want a ticket id", result)
	}
	ticket, err := testfix.OpenStore(t, dataDir).Ticket(int64(id))
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Project.Path != repo {
		t.Errorf("ticket project = %q, want %q", ticket.Project.Path, repo)
	}
}

func TestRPCTicketCreateKeepsActionNamesAsCreationData(t *testing.T) {
	dataDir := t.TempDir()
	repo := testfix.Repo(t, repoBranch)
	for _, title := range []string{"runs", "create", "accept"} {
		request := fmt.Sprintf(`{"jsonrpc":"2.0","method":"ticket.create","params":{"args":[%q,"body; $(literal)"],"project":%q},"id":1}`, title, repo)
		out, err := rpcIn(t, dataDir, t.TempDir(), request)
		if err != nil {
			t.Fatal(err)
		}
		result, ok := rpcObject(t, out)["result"].(map[string]any)
		if !ok {
			t.Fatalf("response = %s, want a ticket result", out)
		}
		ticket, err := testfix.OpenStore(t, dataDir).Ticket(int64(result["id"].(float64)))
		if err != nil {
			t.Fatal(err)
		}
		if ticket.Title != title || proseOfTicket(t, dataDir, ticket.ID) != "body; $(literal)" {
			t.Errorf("ticket = %#v, prose %q", ticket, proseOfTicket(t, dataDir, ticket.ID))
		}
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

// rpcDocument reads a command's document from the protocol result.
func rpcDocument(t *testing.T, dataDir, workDir, method string, args ...string) map[string]any {
	t.Helper()
	request, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": map[string]any{"args": append([]string{}, args...)}, "id": 1})
	if err != nil {
		t.Fatal(err)
	}
	out, err := rpcIn(t, dataDir, workDir, string(request))
	if err != nil {
		t.Fatal(err)
	}
	response := rpcObject(t, out)
	result, ok := response["result"].(map[string]any)
	if !ok {
		t.Fatalf("response = %#v, want an object result", response)
	}
	return result
}

func TestJSONFlagsAreRemoved(t *testing.T) {
	for _, command := range []string{"", "show", "version"} {
		t.Run(command, func(t *testing.T) {
			args := []string{"--json"}
			if command != "" {
				args = append([]string{command}, args...)
			}
			_, err := runIn(t, t.TempDir(), t.TempDir(), args...)
			if err == nil || !strings.Contains(err.Error(), "unknown flag: --json") {
				t.Fatalf("error = %v, want unknown json flag", err)
			}
		})
	}
}
