// Version and schema information lets external clients check compatibility
// with this binary.

package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// Version is set at build time with -ldflags -X
// github.com/alcubie/delegator/internal/cli.Version=<value>. The Makefile
// uses git describe; unset builds report dev.
var Version = "dev"

// jsonSchema versions the structured command results. Increment it when
// removing a key or changing its name or type. Additive keys are compatible
// because consumers ignore unknown fields.
const jsonSchema = 1

// versionJSON exposes the binary version for display, the schema version for
// compatibility checks, and the resolved data directory so that a client can
// watch it without repeating dg's default-path rules.
type versionJSON struct {
	Version string `json:"version"`
	Schema  int    `json:"schema"`
	DataDir string `json:"data_dir"`
}

// versionResultSchema describes versionJSON for RPC discovery. Additional
// properties stay valid because adding fields does not change jsonSchema.
func versionResultSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"version":  map[string]any{"type": "string"},
			"schema":   map[string]any{"type": "integer"},
			"data_dir": map[string]any{"type": "string"},
		},
		"required":             []string{"version", "schema", "data_dir"},
		"additionalProperties": true,
	}
}

// versionCommand returns the command for dg version.
func versionCommand(dataDir *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "version",
		Short:   "Show the version of dg.",
		Long:    "Show the dg build version without opening or changing a Delegator instance.",
		Example: `  dg version`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			value := versionJSON{Version: Version, Schema: jsonSchema, DataDir: *dataDir}
			return writeValue(cmd.OutOrStdout(), value, false, func(out io.Writer) {
				fmt.Fprintln(out, "dg", Version)
			})
		},
	}
	return rpcOperationCommand("version", cmd)
}
