package run

import (
	"os"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/store"
	"github.com/alcubie/delegator/internal/testfix"
)

// liveRun returns a run that a reconcile must leave alone: this program holds
// its process id, and it started after the last boot of the computer.
func liveRun() store.Run {
	return store.Run{PID: os.Getpid(), StartedAt: time.Now()}
}

// bootBefore gives a boot time before the start of a run, which is the boot
// this run belongs to.
func bootBefore(r store.Run) time.Time {
	return r.StartedAt.Add(-time.Hour)
}

// The boot time is the one answer that a restart of the computer cannot fool.
// Each process id is free after a restart, and the reconcile that asks is
// itself a dg program, so a dg that got the old number would see a live
// supervisor and leave the ticket in running for ever.
func TestRunningOnARunFromBeforeTheBoot(t *testing.T) {
	r := liveRun()
	boot := r.StartedAt.Add(time.Second)

	if running(r, boot, time.Now(), time.Hour) {
		t.Error("a run that began before the boot is running, want over")
	}
}

// Signal 0 asks the operating system whether a process id has a program. No
// program holds the id here, so the supervisor is gone.
func TestRunningOnARunWhoseProcessIsNotThere(t *testing.T) {
	r := liveRun()
	r.PID = testfix.FreePID(t)

	if running(r, bootBefore(r), time.Now(), time.Hour) {
		t.Error("a run whose process id is free is running, want over")
	}
}

// The timeout is the backstop of section 6.3: whatever the process id says, a
// run that has taken longer than the person allows is over.
func TestRunningOnARunThatIsPastTheTimeout(t *testing.T) {
	r := liveRun()
	now := r.StartedAt.Add(90 * time.Minute)

	if running(r, bootBefore(r), now, time.Hour) {
		t.Error("a run past the timeout is running, want over")
	}
}

// A supervisor that is there, on a run of this boot that is inside the
// timeout, holds its ticket. The command that asked leaves it alone.
func TestRunningOnARunThatIsGoing(t *testing.T) {
	r := liveRun()

	if !running(r, bootBefore(r), r.StartedAt.Add(time.Minute), time.Hour) {
		t.Error("a run of a live supervisor is over, want running")
	}
}

// A timeout of nothing is no timeout, and not a timeout that every run is
// past. The person who writes 0 in the config asks for no limit, and a run
// that is marked failed the moment it starts would be the other reading.
func TestRunningWithNoTimeout(t *testing.T) {
	r := liveRun()
	now := r.StartedAt.Add(100 * time.Hour)

	if !running(r, bootBefore(r), now, 0) {
		t.Error("a run is over with no timeout, want running")
	}
}

func TestAliveOnTheProgramThatAsks(t *testing.T) {
	if !alive(os.Getpid()) {
		t.Error("this program is not alive")
	}
}

func TestAliveOnAProcessIDThatIsFree(t *testing.T) {
	if alive(testfix.FreePID(t)) {
		t.Error("a free process id is alive")
	}
}

// A run of the store before the table runs held its process id, and a row that
// holds none reads as 0. Signal 0 to the id 0 reaches every program of the
// group of the caller, so the answer is that nothing is there, and it must be
// given without asking.
func TestAliveOnNoProcessID(t *testing.T) {
	for _, pid := range []int{0, -1} {
		if alive(pid) {
			t.Errorf("the process id %d is alive", pid)
		}
	}
}

// The boot time is a time in the past, and every run of the store began after
// it. A zero time would make each run look older than the boot and fail the
// whole queue.
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

// The ticket of a run that is over becomes failed, and the run gets the end
// time that its supervisor never wrote. The timeout is the rule this test
// gives, because the process id of the claim is this test, which is alive.
func TestReconcileFailsATicketWhoseRunIsOver(t *testing.T) {
	dataDir, id := queuedTicket(t, "the first")
	s := testfix.OpenStore(t, dataDir)
	if _, err := s.Claim(id, "delegator/1-the-first"); err != nil {
		t.Fatal(err)
	}

	launch, _ := testfix.RecordingLaunch(t)
	if err := Reconcile(s, launch, time.Nanosecond); err != nil {
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

// The supervisor of this claim is the test that is running, so the reconcile
// of a command that runs beside a supervisor leaves the ticket where it is.
func TestReconcileLeavesATicketWhoseRunIsGoing(t *testing.T) {
	dataDir, id := queuedTicket(t, "the first")
	s := testfix.OpenStore(t, dataDir)
	if _, err := s.Claim(id, "delegator/1-the-first"); err != nil {
		t.Fatal(err)
	}

	launch, _ := testfix.RecordingLaunch(t)
	if err := Reconcile(s, launch, time.Hour); err != nil {
		t.Fatal(err)
	}

	if got := testfix.ReadTicket(t, dataDir, id); got.Status != store.Running {
		t.Errorf("status = %q, want %q", got.Status, store.Running)
	}
}

// A ticket in running holds every ticket below it, so the reconcile that
// frees one must start the next. After a restart of the computer no
// supervisor is alive, and the command of the person that finds that out is
// what makes the queue go again.
func TestReconcileStartsTheNextTicketAfterItMarksARun(t *testing.T) {
	dataDir, first := queuedTicket(t, "the first")
	testfix.SecondTicket(t, dataDir)
	s := testfix.OpenStore(t, dataDir)
	if _, err := s.Claim(first, "delegator/1-the-first"); err != nil {
		t.Fatal(err)
	}
	launch, marker := testfix.RecordingLaunch(t)

	if err := Reconcile(s, launch, time.Nanosecond); err != nil {
		t.Fatal(err)
	}

	testfix.WaitFor(t, marker)
}

// A reconcile that found nothing to correct starts nothing. The queue that a
// supervisor holds is going already, and each supervisor starts the next
// ticket as it ends, so a command that adds a start of its own would put two
// runs on one queue that takes one.
func TestReconcileThatMarksNothingStartsNothing(t *testing.T) {
	dataDir, _ := queuedTicket(t, "the first")
	testfix.SecondTicket(t, dataDir)
	s := testfix.OpenStore(t, dataDir)
	launch, marker := testfix.RecordingLaunch(t)

	if err := Reconcile(s, launch, time.Hour); err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Error("a run was started after a reconcile that marked nothing")
	}
}
