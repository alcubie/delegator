package cli

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/alcubie/delegator/internal/testfix"
)

func TestTelemetryTriggerUsesARealDetachedProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the shell marker helper is Unix-specific")
	}
	marker := filepath.Join(t.TempDir(), "detached")
	saved := launchTelemetry
	launchTelemetry = func(dataDir string) *exec.Cmd {
		// Publish the marker only once its contents are complete.
		return exec.Command("sh", "-c", `printf '%s' "$1" > "$2.tmp" && mv "$2.tmp" "$2"`, "--", dataDir, marker)
	}
	t.Cleanup(func() { launchTelemetry = saved })

	triggerTelemetry("/selected/instance")
	if got := testfix.WaitFor(t, marker); got != "/selected/instance" {
		t.Fatalf("detached sender got data dir %q", got)
	}
}
