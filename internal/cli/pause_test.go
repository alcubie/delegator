package cli

import (
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/testfix"
)

func TestPausePausesARunningQueue(t *testing.T) {
	dataDir := t.TempDir()
	s := testfix.OpenStore(t, dataDir)
	running, err := s.IsQueueRunning()
	if err != nil {
		t.Fatal(err)
	}
	if !running {
		t.Error("Queue should default to running; not running")
	}

	out, err := runIn(t, dataDir, t.TempDir(), "pause")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "The queue is paused") {
		t.Errorf("dg paused wrote %q, want it to start with `The queue is paused`", out)
	}

	running, err = s.IsQueueRunning()
	if err != nil {
		t.Fatal(err)
	}
	if running {
		t.Error("Queue should be paused")
	}
}

func TestPauseNoChangeWhenAlreadyPaused(t *testing.T) {
	dataDir := t.TempDir()
	s := testfix.OpenStore(t, dataDir)
	if err := s.PauseQueue(); err != nil {
		t.Fatal(err)
	}

	out, err := runIn(t, dataDir, t.TempDir(), "pause")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "The queue is paused") {
		t.Errorf("dg paused wrote %q, want it to start with `The queue is paused`", out)
	}
	running, err := s.IsQueueRunning()
	if err != nil {
		t.Fatal(err)
	}
	if running {
		t.Error("Queue should be paused")
	}

}

// Pausing keeps active runs alive, so the response must point users to dg
// cancel for stopping them.
func TestPauseNamesTheCommandThatStopsARun(t *testing.T) {
	out, err := runIn(t, t.TempDir(), t.TempDir(), "pause")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "dg cancel") {
		t.Errorf("dg pause wrote %q, want it to name dg cancel", out)
	}
}
