package cli

import (
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const openRPCVersion = "1.4.1"

type openRPCDocument struct {
	OpenRPC string          `json:"openrpc"`
	Info    openRPCInfo     `json:"info"`
	Methods []openRPCMethod `json:"methods"`
}

type openRPCInfo struct {
	Title   string `json:"title"`
	Version string `json:"version"`
}

type openRPCMethod struct {
	Name           string                     `json:"name"`
	Summary        string                     `json:"summary,omitempty"`
	Description    string                     `json:"description,omitempty"`
	ParamStructure string                     `json:"paramStructure,omitempty"`
	Params         []openRPCContentDescriptor `json:"params"`
	Result         openRPCContentDescriptor   `json:"result"`
}

type openRPCContentDescriptor struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Schema      map[string]any `json:"schema"`
	Required    bool           `json:"required,omitempty"`
}

// rpcOpenRPC describes the commands that rpcTarget lets a caller use. It is
// made from the Cobra tree, so a new command or flag cannot be absent from
// discovery merely because a second list was not changed.
func rpcOpenRPC(root *cobra.Command) openRPCDocument {
	methods := []openRPCMethod{rpcDiscoveryMethod()}
	methods = append(methods, rpcOpenRPCMethod("inbox", root))
	rpcVisitCommands(root, func(name string, command *cobra.Command) {
		if !rpcRefusedMethods[name] {
			methods = append(methods, rpcOpenRPCMethod(name, command))
		}
	})
	sort.Slice(methods, func(i, j int) bool { return methods[i].Name < methods[j].Name })
	return openRPCDocument{
		OpenRPC: openRPCVersion,
		Info:    openRPCInfo{Title: "delegator JSON-RPC API", Version: Version},
		Methods: methods,
	}
}

func rpcDiscoveryMethod() openRPCMethod {
	return openRPCMethod{
		Name:        "rpc.discover",
		Description: "Returns an OpenRPC schema as a description of this service.",
		Params:      []openRPCContentDescriptor{},
		Result: openRPCContentDescriptor{
			Name: "OpenRPC Schema",
			Schema: map[string]any{
				"$ref": "https://raw.githubusercontent.com/open-rpc/meta-schema/master/schema.json",
			},
		},
	}
}

func rpcOpenRPCMethod(name string, command *cobra.Command) openRPCMethod {
	method := openRPCMethod{
		Name:           name,
		Summary:        command.Short,
		ParamStructure: "by-name",
		Params:         []openRPCContentDescriptor{},
		Result: openRPCContentDescriptor{
			Name:   "result",
			Schema: rpcResultSchema(name),
		},
	}

	minimum, maximum := rpcArgumentBounds(command.Use)
	if maximum > 0 {
		use := command.CommandPath()
		if arguments := strings.TrimSpace(strings.TrimPrefix(command.Use, command.Name())); arguments != "" {
			use += " " + arguments
		}
		schema := map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": []string{"string", "number", "boolean", "null"},
			},
			"minItems": minimum,
			"maxItems": maximum,
		}
		method.Params = append(method.Params, openRPCContentDescriptor{
			Name:        "args",
			Description: "The command arguments in the order shown by `" + use + "`.",
			Schema:      schema,
			Required:    minimum > 0,
		})
	}

	var required, optional []openRPCContentDescriptor
	rpcVisitFlags(command, func(flag *pflag.Flag) {
		if flag.Hidden || command.Name() == "edit" && flag.Name == "editor" {
			return
		}
		description := flag.Usage
		if flag.Name == "body-file" {
			description += " Standard input (-) is not available through RPC."
		}
		param := openRPCContentDescriptor{
			Name:        flag.Name,
			Description: description,
			Schema:      rpcFlagSchema(flag),
		}
		if _, param.Required = flag.Annotations[cobra.BashCompOneRequiredFlag]; param.Required {
			required = append(required, param)
		} else {
			optional = append(optional, param)
		}
	})
	// OpenRPC requires every required parameter to precede every optional one.
	if minimum == 0 && len(method.Params) != 0 {
		args := method.Params[0]
		method.Params = append(required, args)
		method.Params = append(method.Params, optional...)
	} else {
		method.Params = append(method.Params, required...)
		method.Params = append(method.Params, optional...)
	}
	return method
}

// rpcResultSchema is the explicit list of command results described by
// discovery. Commands without a published contract retain the empty schema.
func rpcResultSchema(name string) map[string]any {
	switch name {
	case "accept", "ticket":
		return ticketIDResultSchema()
	case "inbox":
		return inboxResultSchema()
	case "list":
		return listResultSchema()
	case "show":
		return showResultSchema()
	case "version":
		return versionResultSchema()
	default:
		return map[string]any{}
	}
}

// rpcArgumentBounds reads Cobra's conventional Use form. The JSON-RPC API
// carries positional arguments in one array, because their names are not JSON
// object keys in the command-line API.
func rpcArgumentBounds(use string) (minimum, maximum int) {
	fields := strings.Fields(use)
	for i := 1; i < len(fields); i++ {
		field := fields[i]
		if strings.HasPrefix(field, "-") {
			if i+1 < len(fields) && strings.HasPrefix(fields[i+1], "<") {
				i++
			}
			continue
		}
		switch {
		case strings.HasPrefix(field, "<"):
			minimum++
			maximum++
		case strings.HasPrefix(field, "["):
			maximum++
		}
	}
	return minimum, maximum
}

func rpcVisitFlags(command *cobra.Command, visit func(*pflag.Flag)) {
	seen := map[string]bool{}
	for current := command; current != nil; current = current.Parent() {
		current.LocalNonPersistentFlags().VisitAll(func(flag *pflag.Flag) {
			if !seen[flag.Name] {
				seen[flag.Name] = true
				visit(flag)
			}
		})
		current.PersistentFlags().VisitAll(func(flag *pflag.Flag) {
			if !seen[flag.Name] {
				seen[flag.Name] = true
				visit(flag)
			}
		})
	}
}

func rpcFlagSchema(flag *pflag.Flag) map[string]any {
	switch flag.Value.Type() {
	case "bool":
		return map[string]any{"type": "boolean"}
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "count":
		return map[string]any{"type": "integer"}
	case "float32", "float64":
		return map[string]any{"type": "number"}
	case "intSlice", "int32Slice", "int64Slice", "uintSlice":
		return map[string]any{"type": "array", "items": map[string]any{"type": "integer"}}
	case "float32Slice", "float64Slice":
		return map[string]any{"type": "array", "items": map[string]any{"type": "number"}}
	case "stringSlice", "stringArray":
		return map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	case "mode":
		return map[string]any{"type": "string", "enum": []string{"always", "never", "auto"}}
	default:
		return map[string]any{"type": "string"}
	}
}
