package run

import (
	"os/exec"
	"syscall"

	"github.com/alcubie/delegator/internal/store"
)

// Next starts a run for the first ticket of the queue, if the queue holds one
// and no run is active. It returns once the program has started, and does not
// wait for it: the caller is a command a person typed, or a supervisor that is
// about to exit, and neither should stay alive for the length of a run. One run
// at a time: a ticket in running, whatever its project, means nothing starts.
//
// launch returns the command for one ticket. dg passes its own executable with
// "run <id>", and a test passes something it can observe.
func Next(dataDir string, launch func(id int64) *exec.Cmd) error {
	return store.With(dataDir, func(s *store.Store) error {

		running, err := s.IsQueueRunning()
		if err != nil {
			return err
		}
		if !running {
			return nil
		}

		open, err := s.OpenTickets()
		if err != nil {
			return err
		}
		for _, t := range open {
			if t.Status == store.Running {
				return nil
			}
		}

		queue, err := s.ListQueue()
		if err != nil {
			return err
		}
		if len(queue) == 0 {
			return nil
		}
		return detach(launch(queue[0].ID))
	})
}

// detach starts cmd so that it outlives the program that started it. The
// child gets a session of its own, so the hangup of a closed terminal and the
// interrupt of a Ctrl-C, which go to the session and to the foreground process
// group, do not reach it. It gets none of the parent's standard streams,
// because a shell waiting on the parent's output would otherwise wait for the
// whole run; the run writes its own log. The process handle is released
// because nothing will wait on it.
func detach(cmd *exec.Cmd) error {
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
