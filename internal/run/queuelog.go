package run

import (
	"fmt"
	"os"
	ossignal "os/signal"
	"path/filepath"
	"syscall"
	"time"
)

var terminate = func(sig os.Signal) {
	ossignal.Reset(sig)
	if p, err := os.FindProcess(os.Getpid()); err == nil && p.Signal(sig) == nil {
		return
	}
	os.Exit(1)
}

// queueLog appends one line to queue.log in the data directory. A failure to
// write never stops the queue.
func queueLog(dataDir, format string, args ...any) {
	f, err := os.OpenFile(filepath.Join(dataDir, "queue.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().UTC().Format(time.RFC3339), fmt.Sprintf(format, args...))
}

// logSignalBeforeClaim records a SIGTERM or SIGHUP that reaches the process
// and then lets the signal take its default effect. The returned function
// stops the watch.
func logSignalBeforeClaim(dataDir string) (stop func()) {
	ch := make(chan os.Signal, 1)
	done := make(chan struct{})
	ossignal.Notify(ch, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		select {
		case sig := <-ch:
			queueLog(dataDir, "signal pid=%d sig=%s", os.Getpid(), sig)
			terminate(sig)
		case <-done:
		}
	}()
	return func() {
		ossignal.Stop(ch)
		close(done)
	}
}
