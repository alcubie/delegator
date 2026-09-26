package run

import (
	"os"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/config"
	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

// liveRun uses this process and a post-boot start time so reconciliation must
// preserve it.
func liveRun() store.Run {
	return store.Run{PID: os.Getpid(), StartedAt: time.Now()}
}

// bootBefore returns a boot time preceding the run.
func bootBefore(r store.Run) time.Time {
	return r.StartedAt.Add(-time.Hour)
}

// A live reused PID must not preserve a run from before this boot.
func TestRunningOnARunFromBeforeTheBoot(t *testing.T) {
	r := liveRun()
	boot := r.StartedAt.Add(time.Second)

	if running(r, boot, time.Now(), time.Hour) {
		t.Error("a run that began before the boot is running, want over")
	}
}

func TestRunningOnARunWhoseProcessIsNotThere(t *testing.T) {
	r := liveRun()
	r.PID = testfix.FreePID(t)

	if running(r, bootBefore(r), time.Now(), time.Hour) {
		t.Error("a run whose process id is free is running, want over")
	}
}

// Timeout ends stale runs even if their PIDs are still alive.
func TestRunningOnARunThatIsPastTheTimeout(t *testing.T) {
	r := liveRun()
	now := r.StartedAt.Add(90 * time.Minute)

	if running(r, bootBefore(r), now, time.Hour) {
		t.Error("a run past the timeout is running, want over")
	}
}

func TestRunningOnARunThatIsGoing(t *testing.T) {
	r := liveRun()

	if !running(r, bootBefore(r), r.StartedAt.Add(time.Minute), time.Hour) {
		t.Error("a run of a live supervisor is over, want running")
	}
}

// Zero disables the time limit rather than expiring every run.
func TestRunningWithNoTimeout(t *testing.T) {
	r := liveRun()
	now := r.StartedAt.Add(100 * time.Hour)

	if !running(r, bootBefore(r), now, 0) {
		t.Error("a run is over with no timeout, want running")
	}
}

// Reject absent or invalid PIDs before signal 0 can probe the caller's group.
func TestAliveOnNoProcessID(t *testing.T) {
	for _, pid := range []int{0, -1} {
		if alive(pid) {
			t.Errorf("the process id %d is alive", pid)
		}
	}
}

// Boot time must be a nonzero timestamp in the past.
func TestBootTime(t *testing.T) {
	boot, err := bootTime()
	if err != nil {
		t.Fatal(err)
	}
	if boot.After(time.Now()) {
		t.Errorf("the boot time is %s, which is in the future", boot)
	}
	if boot.Before(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("the boot time is %s, want a time of this computer", boot)
	}
}

// Age the run past a real one-minute setting. Its PID belongs to this test,
// so timeout is the only reason to fail it.
func TestReconcileFailsATicketWhoseRunIsOver(t *testing.T) {
	dataDir, id := queuedTicket(t, "the first")
	s := testfix.OpenStore(t, dataDir)
	if _, err := s.Claim(id, "delegator/1-the-first"); err != nil {
		t.Fatal(err)
	}
	testfix.AgeRun(t, dataDir, id, 2*time.Minute)

	launch, _ := testfix.RecordingLaunch(t)
	if err := Reconcile(s, launch, config.Config{Runs: 1, TimeoutMinutes: 1}); err != nil {
		t.Fatal(err)
	}

	if got := testfix.ReadTicket(t, dataDir, id); got.Status != store.Failed {
		t.Errorf("status = %q, want %q", got.Status, store.Failed)
	}
	run, err := s.Run(id)
	if err != nil {
		t.Fatal(err)
	}
	if run.EndedAt.IsZero() {
		t.Error("the run of the failed ticket has no end time")
	}
}

func TestReconcileLeavesATicketWhoseRunIsGoing(t *testing.T) {
	dataDir, id := queuedTicket(t, "the first")
	s := testfix.OpenStore(t, dataDir)
	if _, err := s.Claim(id, "delegator/1-the-first"); err != nil {
		t.Fatal(err)
	}

	launch, _ := testfix.RecordingLaunch(t)
	if err := Reconcile(s, launch, config.Config{Runs: 1, TimeoutMinutes: 60}); err != nil {
		t.Fatal(err)
	}

	if got := testfix.ReadTicket(t, dataDir, id); got.Status != store.Running {
		t.Errorf("status = %q, want %q", got.Status, store.Running)
	}
}

// Recovering a stale run must trigger newly eligible work, including after a
// machine restart.
func TestReconcileStartsTheNextTicketAfterItMarksARun(t *testing.T) {
	dataDir, first := queuedTicket(t, "the first")
	testfix.SecondTicket(t, dataDir)
	s := testfix.OpenStore(t, dataDir)
	if _, err := s.Claim(first, "delegator/1-the-first"); err != nil {
		t.Fatal(err)
	}
	testfix.AgeRun(t, dataDir, first, 2*time.Minute)
	launch, marker := testfix.RecordingLaunch(t)

	if err := Reconcile(s, launch, config.Config{Runs: 1, TimeoutMinutes: 1}); err != nil {
		t.Fatal(err)
	}

	testfix.WaitForStarts(t, marker, 1)
}

// Do not trigger extra supervisors when reconciliation changes nothing.
func TestReconcileThatMarksNothingStartsNothing(t *testing.T) {
	dataDir, _ := queuedTicket(t, "the first")
	testfix.SecondTicket(t, dataDir)
	s := testfix.OpenStore(t, dataDir)
	launch, marker := testfix.RecordingLaunch(t)

	if err := Reconcile(s, launch, config.Config{Runs: 1, TimeoutMinutes: 60}); err != nil {
		t.Fatal(err)
	}

	testfix.WaitForStarts(t, marker, 0)
}
