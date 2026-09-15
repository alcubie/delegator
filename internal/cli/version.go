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

// jsonSchema is the version of the JSON that dg --json and dg show --json
// write. It goes up by one when a key of either changes its name or its type,
// or goes away; a new key leaves it where it is, because a reader that does not
// know the key ignores it. §9.3 says so to the reader of the document.
const jsonSchema = 1

// versionJSON is what dg version --json writes. A program reads version to
// show it to a person, and schema to decide whether it can read the rest.
type versionJSON struct {
	Version string `json:"version"`
	Schema  int    `json:"schema"`
}

// versionCommand returns the command for dg version.
func versionCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show the version of dg.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return writeValue(cmd.OutOrStdout(), versionJSON{Version: Version, Schema: jsonSchema}, asJSON, func(out io.Writer) {
				fmt.Fprintln(out, "dg", Version)
			})
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false,
		"write the version as one JSON object")
	return cmd
}
