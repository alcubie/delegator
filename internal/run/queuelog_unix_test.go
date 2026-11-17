//go:build unix

package run

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestASupervisorSignalledBeforeItClaimsWritesASignalLine(t *testing.T) {
	dataDir := t.TempDir()
	exited := make(chan os.Signal, 1)
	saved := terminate
	terminate = func(sig os.Signal) { exited <- sig }
	t.Cleanup(func() { terminate = saved })
	stop := logSignalBeforeClaim(dataDir)
	defer stop()

	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}

	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("the supervisor did not handle SIGTERM")
	}
	want := fmt.Sprintf(" signal pid=%d sig=terminated\n", os.Getpid())
	if got := readQueueLog(t, dataDir); !strings.HasSuffix(got, want) {
		t.Errorf("queue.log = %q, want it to end with %q", got, want)
	}
}
