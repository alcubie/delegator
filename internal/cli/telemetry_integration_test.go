package cli

import (
	"errors"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alcubie/delegator/internal/store"
)

func TestConfigConsentSendsImmediatelyWithoutChangingCommandResult(t *testing.T) {
	dataDir := t.TempDir()
	var sends atomic.Int32
	savedSend, savedLaunch := sendTelemetry, launchTelemetry
	sendTelemetry = func(*store.Store, time.Time, string) error {
		sends.Add(1)
		return errors.New("collector unavailable")
	}
	launchTelemetry = func(string) *exec.Cmd { return exec.Command("true") }
	t.Cleanup(func() { sendTelemetry, launchTelemetry = savedSend, savedLaunch })

	if out, err := runIn(t, dataDir, t.TempDir(), "config", "set", "telemetry", "true"); err != nil || out != "" {
		t.Fatalf("config set output = %q, err = %v", out, err)
	}
	if sends.Load() != 1 {
		t.Fatalf("immediate sends = %d, want 1", sends.Load())
	}
	out, err := runIn(t, dataDir, t.TempDir(), "config", "get", "telemetry")
	if err != nil || out != "true\n" {
		t.Fatalf("config get output = %q, err = %v", out, err)
	}
}
