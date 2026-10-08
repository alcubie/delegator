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

// rpcRefusedOperations need a person's terminal or own the remote-call
// transport. They retain identities so aliases inherit the same treatment.
var rpcRefusedOperations = map[string]bool{"chat": true, "init": true, "rpc": true}

func rpcCallable(command *cobra.Command) bool {
	operation, ok := rpcOperation(command)
	return ok && !rpcRefusedOperations[operation]
}
