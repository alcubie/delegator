package cli

import "github.com/spf13/cobra"

// rpcOperationAnnotation gives every command factory a stable remote-call
// identity. Command paths can then change or gain aliases without changing
// operation-specific policy and result contracts.
const rpcOperationAnnotation = "delegator.io/rpc-operation"

// rpcOperationCommand marks a command as an operation that remote callers may
// discover. Namespace commands deliberately have no operation annotation.
func rpcOperationCommand(operation string, command *cobra.Command) *cobra.Command {
	if command.Annotations == nil {
		command.Annotations = map[string]string{}
	}
	command.Annotations[rpcOperationAnnotation] = operation
	return command
}

func rpcOperation(command *cobra.Command) (string, bool) {
	if command.Annotations == nil {
		return "", false
	}
	operation, ok := command.Annotations[rpcOperationAnnotation]
	return operation, ok
}

// rpcRefusedOperations need a person's terminal, own the remote-call
// transport, or need the environment of the dg ticket chat process that
// started them. They retain identities so aliases inherit the same
// treatment. The tool of diff outlives the request, which the transport
// cannot hold open.
var rpcRefusedOperations = map[string]bool{
	"chat":  true,
	"diff":  true,
	"init":  true,
	"rerun": true,
	"rpc":   true,
}

// projectPathOperations take the directory of a project as their one
// positional argument.
var projectPathOperations = map[string]bool{
	"project.add":     true,
	"project.archive": true,
	"project.restore": true,
}

func rpcCallable(command *cobra.Command) bool {
	operation, ok := rpcOperation(command)
	return ok && !rpcRefusedOperations[operation]
}
