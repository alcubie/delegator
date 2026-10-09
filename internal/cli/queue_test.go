package cli

import (
	"strings"
	"testing"

	"github.com/alcubie/delegator/internal/testfix"
)

func TestBareQueueShowsGroupHelpWithoutChangingState(t *testing.T) {
	dataDir := t.TempDir()
	s := testfix.OpenStore(t, dataDir)
	out, err := runIn(t, dataDir, t.TempDir(), "queue")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Available Commands:", "pause", "start"} {
		if !strings.Contains(out, want) {
			t.Errorf("queue help does not contain %q:\n%s", want, out)
		}
	}
	if running, err := s.IsQueueRunning(); err != nil {
		t.Fatal(err)
	} else if !running {
		t.Error("bare queue paused the queue")
	}
}

func TestQueuePauseMatchesRootCompatibilityForm(t *testing.T) {
	for _, prefix := range [][]string{{"pause"}, {"queue", "pause"}} {
		t.Run(strings.Join(prefix, " "), func(t *testing.T) {
			dataDir := t.TempDir()
			s := testfix.OpenStore(t, dataDir)
			out, err := runIn(t, dataDir, t.TempDir(), prefix...)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(out, "The queue is paused") {
				t.Errorf("dg %s wrote %q", strings.Join(prefix, " "), out)
			}
			if running, err := s.IsQueueRunning(); err != nil {
				t.Fatal(err)
			} else if running {
				t.Error("the queue is still running")
			}
		})
	}
}

func TestQueueStartMatchesRootCompatibilityForm(t *testing.T) {
	for _, prefix := range [][]string{{"start"}, {"queue", "start"}} {
		t.Run(strings.Join(prefix, " "), func(t *testing.T) {
			dataDir := testfix.XDGDataDir(t)
			s, _, repo := queuedTicket(t, dataDir)
			if err := s.PauseQueue(); err != nil {
				t.Fatal(err)
			}
			launcher, record := testfix.RecordingLaunch(t)
			useLaunch(t, launcher)

			out, err := runIn(t, dataDir, repo, prefix...)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(out, "The queue is running") {
				t.Errorf("dg %s wrote %q", strings.Join(prefix, " "), out)
			}
			if running, err := s.IsQueueRunning(); err != nil {
				t.Fatal(err)
			} else if !running {
				t.Error("the queue is still paused")
			}
			testfix.WaitForStarts(t, record, 1)
		})
	}
}

func TestQueueCommandsUseFreshInstances(t *testing.T) {
	root := Root(t.TempDir())
	for _, name := range []string{"pause", "start"} {
		legacy, _, err := root.Find([]string{name})
		if err != nil {
			t.Fatal(err)
		}
		grouped, _, err := root.Find([]string{"queue", name})
		if err != nil {
			t.Fatal(err)
		}
		legacy.Flags().Bool("compatibility-only", false, "test flag")
		if grouped.Flags().Lookup("compatibility-only") != nil {
			t.Errorf("adding a flag to root %s also changed queue %s", name, name)
		}
	}
}

func TestQueueHelpFavorsGroupedCommandsWithoutWarnings(t *testing.T) {
	for _, name := range []string{"pause", "start"} {
		out, errOut, err := runInOutputs(t, t.TempDir(), t.TempDir(), "", name, "--help")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "dg queue "+name) {
			t.Errorf("dg %s help does not point to dg queue %s:\n%s", name, name, out)
		}
		if errOut != "" {
			t.Errorf("dg %s help warned on stderr: %q", name, errOut)
		}

		out, errOut, err = runInOutputs(t, t.TempDir(), t.TempDir(), "", "queue", name, "--help")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "dg queue "+name) || errOut != "" {
			t.Errorf("dg queue %s help = %q, stderr = %q", name, out, errOut)
		}
	}
}
