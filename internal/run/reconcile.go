package run

import (
	"os/exec"
	"time"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/store"
)

// Reconcile marks tickets failed when their supervisors are gone and records
// missing run end times. Commands call it to recover from crashes and machine
// restarts. If it frees capacity, it triggers eligible queued work; otherwise
// it leaves scheduling to existing supervisors. cfg supplies the timeout and
// capacity limits.
func Reconcile(s *store.Store, launch func() *exec.Cmd, cfg config.Config) error {
	boot, err := bootTime()
	if err != nil {
		return err
	}
	now := time.Now()
	timeout := cfg.Timeout()
	marked, err := s.Reconcile(func(r store.Run) bool { return running(r, boot, now, timeout) })
	if err != nil {
		return err
	}
	if marked == 0 {
		return nil
	}
	return Next(s, cfg, launch)
}

// running checks the supervisor PID, boot time, and run timeout. A run is
// over if it began before this boot, its process is gone, or its timeout has
// elapsed. The boot check prevents reused PIDs after a restart from keeping
// tickets running; the timeout covers PID reuse within one boot. Zero timeout
// means no time limit.
func running(r store.Run, boot, now time.Time, timeout time.Duration) bool {
	if r.StartedAt.Before(boot) {
		return false
	}
	if timeout > 0 && now.Sub(r.StartedAt) >= timeout {
		return false
	}
	return alive(r.PID)
}
