package cli

import (
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/testfix"
)

func TestStartStartsAPausedQueue(t *testing.T) {
	dataDir := t.TempDir()
	s := testfix.OpenStore(t, dataDir)
	if err := s.PauseQueue(); err != nil {
		t.Fatal(err)
	}

	out, err := runIn(t, dataDir, t.TempDir(), "start")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "The queue is running") {
		t.Errorf("dg start wrote %q, want it to start with `The queue is running`", out)
	}
	running, err := s.IsQueueRunning()
	if err != nil {
		t.Fatal(err)
	}
	if !running {
		t.Error("Queue should be running")
	}
}

// dg start resumes the queue and starts a supervisor. It names no ticket: the
// supervisor claims the first ticket of the queue for itself.
func TestStartStartsARunWhenNothingIsRunning(t *testing.T) {
	dataDir := testfix.XDGDataDir(t)
	s, _, repo := queuedTicket(t, dataDir)
	if err := s.PauseQueue(); err != nil {
		t.Fatal(err)
	}
	l, marker := testfix.RecordingLaunch(t)
	useLaunch(t, l)

	_, err := runIn(t, dataDir, repo, "start")
	if err != nil {
		t.Fatal(err)
	}

	testfix.WaitForStarts(t, marker, 1)
}

// The limit is the one the config file holds. dg start reads it and starts a
// supervisor for each free slot, so a person who writes runs = 2 gets two runs
// from one command.
func TestStartStartsARunForEachSlotTheConfigGives(t *testing.T) {
	dataDir := testfix.XDGDataDir(t)
	s, _, repo := queuedTicket(t, dataDir)
	testfix.SecondTicket(t, dataDir)
	writeConfig(t, "runs = 2\n")
	if err := s.PauseQueue(); err != nil {
		t.Fatal(err)
	}
	l, marker := testfix.RecordingLaunch(t)
	useLaunch(t, l)

	if _, err := runIn(t, dataDir, repo, "start"); err != nil {
		t.Fatal(err)
	}

	testfix.WaitForStarts(t, marker, 2)
}

func TestStartNoChangeWhenAlreadyRunning(t *testing.T) {
	dataDir := t.TempDir()
	s := testfix.OpenStore(t, dataDir)

	out, err := runIn(t, dataDir, t.TempDir(), "start")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "The queue is running") {
		t.Errorf("dg start wrote %q, want it to start with `The queue is running`", out)
	}
	running, err := s.IsQueueRunning()
	if err != nil {
		t.Fatal(err)
	}
	if !running {
		t.Error("the queue should still be running")
	}
}
