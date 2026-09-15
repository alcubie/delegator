// The stop of a run on Windows. A name that ends in _windows is a build
// constraint of the go tool, so this file is in a build for Windows and in no
// other, and the stop that Stop calls is the one for the system the build is
// for.

package run

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// taskkillNoProcess is the exit code that taskkill gives when no process holds
// the process id. It is the code of a run that ended between the read of its
// row and the stop, which is the ordinary end of a run and not a fault.
const taskkillNoProcess = 128

// stop ends a supervisor and each program below it with taskkill. Windows has
// no signal to a process group from another process. GenerateConsoleCtrlEvent
// reaches the processes of one console, and detach leaves the supervisor with
// no console at all, so the event of a Ctrl-C cannot go to it.
//
// The flag /T ends the process and each process below it, which taskkill finds
// from the parent of each process of the computer. The flag /F ends them at
// once.
//
// grace is not used. The two signals on Unix are for a program that has a
// handler for SIGTERM, and nothing here can ask the supervisor to stop:
// taskkill without /F sends a close to a window, and the supervisor has no
// window and no console. There is one step, and nothing to wait for between
// two.
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
