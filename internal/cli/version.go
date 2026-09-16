// The version of the binary, for a program that starts dg and has to know that
// this dg writes the JSON it expects. The desktop GUI carries its own copy of
// dg and uses the one on the PATH when the two agree, so it reads the version
// of each at its start.

package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// Version is the version of this binary. A build sets it with
// -ldflags -X github.com/alcubie/delegator/internal/cli.Version=<value>, and
// the Makefile takes the value from git describe. A build that sets nothing
// keeps dev, which says the binary came from a tree and not from a release.
var Version = "dev"

// jsonSchema is the version of the documents that dg rpc
// writes. It goes up by one when a key changes its name or its type,
// or goes away; a new key leaves it where it is, because a reader that does not
// know the key ignores it. §9.3 says so to the reader of the document.
const jsonSchema = 1

// versionJSON is the result of the version method of dg rpc. A program reads version to
// show it to a person, and schema to decide whether it can read the rest.
type versionJSON struct {
	Version string `json:"version"`
	Schema  int    `json:"schema"`
}

// versionCommand returns the command for dg version.
func versionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show the version of dg.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return writeValue(cmd.OutOrStdout(), versionJSON{Version: Version, Schema: jsonSchema}, false, func(out io.Writer) {
				fmt.Fprintln(out, "dg", Version)
			})
		},
	}
	return cmd
}
