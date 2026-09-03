package cli

import (
	"strconv"
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

func TestStartStartsARunWhenNothingIsRunning(t *testing.T) {
	dataDir := testfix.XDGDataDir(t)
	s, ticketID, repo := queuedTicket(t, dataDir)
	if err := s.PauseQueue(); err != nil {
		t.Fatal(err)
	}
	l, marker := testfix.RecordingLaunch(t)
	useLaunch(t, l)

	_, err := runIn(t, dataDir, repo, "start")
	if err != nil {
		t.Fatal(err)
	}

	if got := testfix.WaitFor(t, marker); got != strconv.FormatInt(ticketID, 10) {
		t.Errorf("started ticket %s, want the new ticket %d", got, ticketID)
	}
}
