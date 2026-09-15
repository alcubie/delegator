// The start of a supervisor apart from the command that launched it.

package run

import "os/exec"

// Detach starts cmd so that it outlives the program that started it. What
// takes the child out of the reach of the terminal is detachAttr, which is
// the one of the system the build is for. It gets none of the parent's
// standard streams, because a shell waiting on the parent's output would
// otherwise wait for the whole run; the run writes its own log. The process
// handle is released because nothing will wait on it.
func Detach(cmd *exec.Cmd) error {
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	cmd.SysProcAttr = detachAttr()
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
