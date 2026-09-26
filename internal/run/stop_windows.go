package run

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// taskkillNoProcess is taskkill's exit code for an absent PID, including a
// run that exited just before cancellation.
const taskkillNoProcess = 128

// stop uses taskkill /T /F to terminate the supervisor and its descendants.
// Detached supervisors have no console for GenerateConsoleCtrlEvent and no
// window for taskkill's non-forced close request. There is therefore no
// graceful step, and grace is unused.
func stop(pid int, grace time.Duration) error {
	out, err := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).CombinedOutput()
	if err == nil {
		return nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == taskkillNoProcess {
		return nil
	}
	return fmt.Errorf("taskkill of the process %d: %w: %s", pid, err, strings.TrimSpace(string(out)))
}
