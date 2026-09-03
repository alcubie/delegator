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
