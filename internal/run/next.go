package run

import (
	"os/exec"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/store"
)

// Next starts a supervisor for each ticket of the queue that a claim could
// take now. It names no ticket: each supervisor reads the queue and claims a
// ticket for itself, in one transaction, so two triggers at the same time
// cannot send two supervisors to one ticket. What Next decides is how many
// supervisors to start.
//
// The count is ClaimableCount, which asks the question the claim asks: the
// free slots of the whole queue, the places each project has left, and the
// links of each ticket. A count of the free slots alone would start
// supervisors for a queue whose tickets are all held back, and each of those
// supervisors calls Next again as it stops, so the trigger that starts one
// more than the claims will find never settles.
//
// It returns once the programs have started, and does not wait for them: the
// caller is a command a person typed, or a supervisor that is about to exit,
// and neither should stay alive for the length of a run.
//
// launch returns the command that starts a supervisor. dg passes its own
// executable with "run", and a test passes something it can observe.
func Next(s *store.Store, cfg config.Config, launch func() *exec.Cmd) error {
	claimable, err := s.ClaimableCount(cfg)
	if err != nil {
		return err
	}
	for range claimable {
		if err := detach(launch()); err != nil {
			return err
		}
	}
	return nil
}

// detach starts cmd so that it outlives the program that started it. What
// takes the child out of the reach of the terminal is detachAttr, which is
// the one of the system the build is for. It gets none of the parent's
// standard streams, because a shell waiting on the parent's output would
// otherwise wait for the whole run; the run writes its own log. The process
// handle is released because nothing will wait on it.
func detach(cmd *exec.Cmd) error {
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	cmd.SysProcAttr = detachAttr()
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
