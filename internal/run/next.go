package run

import (
	"os/exec"
	"syscall"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/store"
)

// Next starts a supervisor for each free slot of the queue, and one at most
// for each ticket the queue holds. It names no ticket: each supervisor reads
// the queue and claims a ticket for itself, in one transaction, so two
// triggers at the same time cannot send two supervisors to one ticket. What
// Next decides is how many supervisors to start, and a supervisor that finds
// no slot by the time it reads the queue stops.
//
// cfg is the config of the person, and cfg.Runs is the limit it reads. A free
// slot is one that no ticket in running and no ticket in ready holds, so a
// limit of one starts a supervisor only for a queue that has nothing open at
// all.
//
// A supervisor can also find no ticket while slots are free: the limit for one
// project is below the limit of the queue, and every ticket left in the queue
// can belong to a project that is at it. Next does not count that, because the
// supervisor asks the same question of each ticket of the queue when it claims.
//
// It returns once the programs have started, and does not wait for them: the
// caller is a command a person typed, or a supervisor that is about to exit,
// and neither should stay alive for the length of a run. What a slot means is
// in the store, with the claim that asks the same question.
//
// launch returns the command that starts a supervisor. dg passes its own
// executable with "run", and a test passes something it can observe.
func Next(s *store.Store, cfg config.Config, launch func() *exec.Cmd) error {
	free, err := s.FreeSlots(cfg)
	if err != nil {
		return err
	}
	if free == 0 {
		return nil
	}

	queue, err := s.ListQueue()
	if err != nil {
		return err
	}
	for range min(free, len(queue)) {
		if err := detach(launch()); err != nil {
			return err
		}
	}
	return nil
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
