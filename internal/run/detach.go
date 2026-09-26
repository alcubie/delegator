package run

import "os/exec"

// Detach starts cmd independently of the launching terminal, using platform-
// specific process attributes. Standard streams are disconnected so shell
// pipelines do not wait for the entire run; the supervisor opens its own log.
// The process handle is released because the caller will not wait for it.
func Detach(cmd *exec.Cmd) error {
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	cmd.SysProcAttr = detachAttr()
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
