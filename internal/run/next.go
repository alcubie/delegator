package run

import (
	"os/exec"
	"syscall"

	"github.com/alcubie/delegator/internal/store"
)

// Next starts a supervisor, if the queue has a ticket and no ticket is in
// running or ready. It names no ticket: the supervisor reads the queue and
// claims a ticket for itself, in one transaction, so two triggers at the same
// time cannot send two supervisors to one ticket. What Next decides is how
// many supervisors to start, and a supervisor that finds no room by the time
// it reads the queue stops.
//
// It returns once the program has started, and does not wait for it: the
// caller is a command a person typed, or a supervisor that is about to exit,
// and neither should stay alive for the length of a run. One ticket at a time:
// a ticket in running or in ready, whatever its project, means nothing starts.
// A ticket in ready is work the person has not examined yet, and the queue
// waits for them to close it.
//
// launch returns the command that starts a supervisor. dg passes its own
// executable with "run", and a test passes something it can observe.
func Next(s *store.Store, launch func() *exec.Cmd) error {
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
		if t.Status == store.Running || t.Status == store.Ready {
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
	return detach(launch())
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
