package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
)

// rpcRefusedMethods are commands a JSON-RPC caller cannot use. chat needs the
// terminal a person is at, and rpc is the endpoint that already owns stdin.
var rpcRefusedMethods = map[string]bool{"chat": true, "rpc": true}

// rpcValueWriter takes the value that a command gives writeValue.  It also
// discards bytes from commands that have no value yet, so nothing a request
// runs reaches the terminal.
type rpcValueWriter struct {
	value any
}

func (*rpcValueWriter) Write(p []byte) (int, error) { return len(p), nil }

type rpcRequest struct {
	JSONRPC string
	Method  string
	Params  json.RawMessage
	ID      json.RawMessage
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  *any            `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
	ID      json.RawMessage `json:"id"`
}

// rpcCommand returns dg rpc.  The command reads exactly one JSON-RPC request
// or batch from standard input and always writes its response on standard
// output, including command and protocol errors.
func rpcCommand(dataDir, workDir string) *cobra.Command {
	return &cobra.Command{
		Use:   "rpc",
		Short: "Run a dg command from a JSON-RPC request on standard input.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := io.ReadAll(cmd.InOrStdin())
			if err != nil {
				return err
			}
			response := rpcResponses(dataDir, workDir, data)
			return writeRPC(cmd.OutOrStdout(), response)
		},
	}
}

// rpcResponses turns one JSON value into the response that belongs to it.  A
// parse error still has a response, which is why dg rpc succeeds whenever it
// could write one.
func rpcResponses(dataDir, workDir string, data []byte) any {
	var raw json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&raw); err != nil {
		return rpcErrorResponse(nil, rpcParseError, "Parse error")
	}
	// A request is one JSON value.  Decode again to reject trailing values
	// rather than silently running only the first; a clean end returns io.EOF.
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return rpcErrorResponse(nil, rpcParseError, "Parse error")
	}

	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return rpcErrorResponse(nil, rpcParseError, "Parse error")
	}
	if trimmed[0] != '[' {
		return rpcResponseFor(dataDir, workDir, raw)
	}

	var batch []json.RawMessage
	if err := json.Unmarshal(raw, &batch); err != nil {
		return rpcErrorResponse(nil, rpcInvalidRequest, "Invalid Request")
	}
	responses := make([]rpcResponse, 0, len(batch))
	for _, request := range batch {
		responses = append(responses, rpcResponseFor(dataDir, workDir, request))
	}
	return responses
}

func writeRPC(out io.Writer, response any) error {
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	return enc.Encode(response)
}

func rpcErrorResponse(id json.RawMessage, code int, message string) rpcResponse {
	return rpcResponse{
		JSONRPC: "2.0",
		Error:   &rpcError{Code: code, Message: message},
		ID:      id,
	}
}

func rpcResultResponse(id json.RawMessage, value any) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", Result: &value, ID: id}
}

func rpcResponseFor(dataDir, workDir string, raw json.RawMessage) rpcResponse {
	request, err := readRPCRequest(raw)
	if err != nil {
		return rpcErrorResponse(nil, rpcInvalidRequest, "Invalid Request")
	}

	root := Root(dataDir, workDir)
	target, err := rpcTarget(root, request.Method)
	if err != nil {
		return rpcErrorResponse(request.ID, rpcMethodNotFound, "Method not found")
	}
	argv, err := rpcArgv(target, request)
	if err != nil {
		return rpcErrorResponse(request.ID, rpcInvalidParams, "Invalid params")
	}

	var value rpcValueWriter
	root.SetOut(&value)
	root.SetErr(io.Discard)
	// stdin belongs to the protocol.  In particular, a request that makes a
	// ticket carries its prose in args rather than asking the command to read
	// the request stream as --body-file -.
	root.SetIn(strings.NewReader(""))
	root.SetArgs(argv)
	if err := root.Execute(); err != nil {
		return rpcErrorResponse(request.ID, ErrorCode(err), err.Error())
	}
	// A command with no value has still succeeded: JSON-RPC requires its
	// result member, and null tells the caller the write happened.
	return rpcResultResponse(request.ID, value.value)
}

func readRPCRequest(raw json.RawMessage) (rpcRequest, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return rpcRequest{}, fmt.Errorf("invalid request")
	}
	var request rpcRequest
	if value, ok := fields["jsonrpc"]; !ok || json.Unmarshal(value, &request.JSONRPC) != nil || request.JSONRPC != "2.0" {
		return rpcRequest{}, fmt.Errorf("jsonrpc must be \"2.0\"")
	}
	if value, ok := fields["method"]; !ok || json.Unmarshal(value, &request.Method) != nil || request.Method == "" {
		return rpcRequest{}, fmt.Errorf("method must name a command")
	}
	request.Params = fields["params"]
	request.ID = fields["id"]
	return request, nil
}

func rpcTarget(root *cobra.Command, method string) (*cobra.Command, error) {
	if rpcRefusedMethods[method] {
		return nil, fmt.Errorf("method %q was not found", method)
	}
	if method == "inbox" {
		return root, nil
	}
	for _, command := range root.Commands() {
		if command.Name() == method {
			return command, nil
		}
	}
	return nil, fmt.Errorf("method %q was not found", method)
}

// rpcArgv translates the parameter object to the ordinary command line that
// cobra parses.  Keeping the translation before Execute means all existing
// flag validation remains the validation of dg itself.
func rpcArgv(command *cobra.Command, request rpcRequest) ([]string, error) {
	params := map[string]json.RawMessage{}
	if request.Params != nil {
		trimmed := bytes.TrimSpace(request.Params)
		if len(trimmed) == 0 || trimmed[0] != '{' || json.Unmarshal(trimmed, &params) != nil {
			return nil, fmt.Errorf("params must be an object")
		}
	}
	// The request consumed standard input before cobra sees this command, so no
	// method can use - as the file that holds its prose.
	if raw, found := params["body-file"]; found {
		if bodyFile, err := rpcString(raw); err == nil && bodyFile == "-" {
			return nil, fmt.Errorf("body-file cannot be standard input")
		}
	}
	// An editor belongs to the person at a terminal; an RPC caller supplies the
	// title or prose itself instead.
	if command.Name() == "edit" {
		if _, found := params["editor"]; found {
			return nil, fmt.Errorf("editor is not available through rpc")
		}
	}

	argv := []string{}
	if rawArgs, found := params["args"]; found {
		var args []json.RawMessage
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return nil, fmt.Errorf("args must be an array")
		}
		for _, arg := range args {
			value, err := rpcString(arg)
			if err != nil {
				return nil, fmt.Errorf("args contains %v", err)
			}
			argv = append(argv, value)
		}
	}

	for name, raw := range params {
		if name == "args" {
			continue
		}
		if !rpcHasFlag(command, name) {
			return nil, fmt.Errorf("%q is not a flag of %s", name, command.Name())
		}
		var array []json.RawMessage
		if err := json.Unmarshal(raw, &array); err == nil && array != nil {
			for _, item := range array {
				value, err := rpcString(item)
				if err != nil {
					return nil, fmt.Errorf("%q contains %v", name, err)
				}
				argv = append(argv, "--"+name, value)
			}
			continue
		}
		var boolean bool
		if err := json.Unmarshal(raw, &boolean); err == nil {
			if boolean {
				argv = append(argv, "--"+name)
			}
			continue
		}
		value, err := rpcString(raw)
		if err != nil {
			return nil, fmt.Errorf("%q is %v", name, err)
		}
		argv = append(argv, "--"+name, value)
	}

	if command == command.Root() {
		return argv, nil
	}
	return append([]string{command.Name()}, argv...), nil
}

func rpcHasFlag(command *cobra.Command, name string) bool {
	for current := command; current != nil; current = current.Parent() {
		if current.Flags().Lookup(name) != nil || current.PersistentFlags().Lookup(name) != nil {
			return true
		}
	}
	return false
}

func rpcString(raw json.RawMessage) (string, error) {
	var value any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return "", err
	}
	switch value := value.(type) {
	case string:
		return value, nil
	case json.Number:
		return value.String(), nil
	case bool:
		return strconv.FormatBool(value), nil
	case nil:
		return "null", nil
	default:
		return "", fmt.Errorf("not a scalar")
	}
}
