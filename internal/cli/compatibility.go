package cli

import (
	"strings"

	"github.com/spf13/cobra"
)

const replacementAnnotation = "delegator.replacement"

// deprecatedCommand keeps old invocations executable but out of public help,
// completion and generated documentation. Cobra's Deprecated field writes to
// the command output, so emit our warning explicitly on stderr instead.
func deprecatedCommand(canonical string, cmd *cobra.Command) *cobra.Command {
	cmd.Hidden = true
	if cmd.Annotations == nil {
		cmd.Annotations = make(map[string]string)
	}
	cmd.Annotations[replacementAnnotation] = canonical
	cmd.Short += " (deprecated; use " + canonical + ")"
	cmd.Long += " Deprecated. The preferred command is `" + canonical + "`."
	validate := cmd.Args
	cmd.Args = func(cmd *cobra.Command, args []string) error {
		cmd.PrintErrf("Warning: %s is deprecated; use %s instead.\n", cmd.CommandPath(), canonical)
		if validate != nil {
			return validate(cmd, args)
		}
		return nil
	}
	cmd.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		cmd.PrintErrf("%s is deprecated; use %s instead. Run %s --help for usage.\n", cmd.CommandPath(), canonical, canonical)
	})
	return cmd
}

// compatibilityCommand also guides callers of the retained remote method.
func compatibilityCommand(canonical string, cmd *cobra.Command) *cobra.Command {
	deprecatedCommand(canonical, cmd)
	method := strings.ReplaceAll(strings.TrimPrefix(canonical, "dg "), " ", ".")
	cmd.Long += " The preferred JSON-RPC method is `" + method + "`."
	return cmd
}
